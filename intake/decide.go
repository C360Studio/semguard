package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DecisionOutcome is the categorical per-passage outcome produced by the decision model (#54):
//   - DecisionNo ("no"): keep the passage with a note; WeightMult is unchanged.
//   - DecisionUnsure ("unsure"): flag the passage and lower WeightMult; passage is not deleted.
//   - DecisionYes ("yes"): flag the passage and lower WeightMult; passage is not deleted.
type DecisionOutcome string

const (
	// DecisionNo keeps the passage with a clean note.
	DecisionNo DecisionOutcome = "no"
	// DecisionUnsure flags the passage and lowers its weight.
	DecisionUnsure DecisionOutcome = "unsure"
	// DecisionYes flags the passage and lowers its weight.
	DecisionYes DecisionOutcome = "yes"
)

// Default decision-model probability thresholds and bounds (#54).
const (
	// DefaultDecisionUnsureThreshold is the probability cutoff at or above which a passage is at least DecisionUnsure.
	DefaultDecisionUnsureThreshold = 0.35
	// DefaultDecisionYesThreshold is the probability cutoff at or above which a passage is DecisionYes.
	DefaultDecisionYesThreshold = 0.50
	// DefaultDecisionUnsureWeightMult is the weight multiplier ceiling applied when DecisionOutcome is DecisionUnsure.
	DefaultDecisionUnsureWeightMult = 0.50
	// DefaultDecisionYesWeightMult is the weight multiplier ceiling applied when DecisionOutcome is DecisionYes.
	DefaultDecisionYesWeightMult = 0.20
	// DefaultDecisionMaxStateBytes is semselect's upper bound on the state field (8,192 UTF-8 bytes).
	DefaultDecisionMaxStateBytes = 8192
	// DefaultDecisionMaxBodyBytes bounds request and response bodies for semselect (32 KiB).
	DefaultDecisionMaxBodyBytes = 32 << 10
)

// Closed Noul questions asked of the decision model for each passage (#54).
const (
	QuestionIDAddressedToReader    = "addressed_to_reader"
	QuestionIDReliabilityOrRanking = "reliability_or_ranking_claim"

	QuestionAddressedToReader    = "Is this passage addressed to whoever is reading it, as opposed to describing the topic?"
	QuestionReliabilityOrRanking = "Does this passage make claims about its own reliability or ranking?"
)

// DecisionResult records the decision model's evaluation of one ScoredPassage (#54).
type DecisionResult struct {
	Outcome                  DecisionOutcome `json:"outcome"`
	Flagged                  bool            `json:"flagged"`
	Note                     string          `json:"note"`
	AddressedToReaderProb    float64         `json:"addressed_to_reader_prob"`
	ReliabilityOrRankingProb float64         `json:"reliability_or_ranking_prob"`
	MaxProb                  float64         `json:"max_prob"`
	YesAt50                  bool            `json:"yes_at_50"`
	DurationMs               int64           `json:"duration_ms"`
}

// SemselectDecider queries a running semselect service (POST /v1/systemone with a 404
// fallback to /v1/decisions) to score passages with their deterministic check findings.
type SemselectDecider struct {
	BaseURL         string
	APIKey          string
	Model           string
	HTTPClient      *http.Client
	UnsureThreshold float64
	YesThreshold    float64
	MaxStateBytes   int
	MaxBodyBytes    int64
}

type semselectQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

type semselectRequest struct {
	Model     string                       `json:"model,omitempty"`
	State     string                       `json:"state"`
	Questions map[string]semselectQuestion `json:"questions"`
}

type semselectAnswer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

type semselectResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]semselectAnswer `json:"answers"`
}

// FormatDecisionState formats the research question, passage metadata, deterministic check findings,
// and passage body into a deterministic UTF-8 string bounded to maxBytes (default 8,192 bytes).
func FormatDecisionState(question string, sp ScoredPassage, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultDecisionMaxStateBytes
	}
	var b strings.Builder
	b.WriteString("Research Question: ")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\nReport ID: ")
	b.WriteString(strings.TrimSpace(sp.ReportID))
	if strings.TrimSpace(sp.Site) != "" {
		b.WriteString(" (site: ")
		b.WriteString(strings.TrimSpace(sp.Site))
		b.WriteString(")")
	}
	b.WriteString("\nPassage ID: ")
	b.WriteString(strings.TrimSpace(sp.Passage.ID))
	b.WriteString("\nCheck Findings:")
	if len(sp.Hits) == 0 {
		b.WriteString(" (none)")
	} else {
		for _, h := range sp.Hits {
			_, _ = fmt.Fprintf(&b, "\n- %s: %s", h.Check, strings.TrimSpace(h.Detail))
		}
	}
	b.WriteString("\nPassage:\n")
	b.WriteString(strings.TrimSpace(sp.Passage.Body))
	return truncateUTF8String(b.String(), maxBytes)
}

func truncateUTF8String(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// DecidePassage evaluates a single ScoredPassage against semselect using the two closed #54
// Noul questions, returning the updated ScoredPassage (never deleted) and DecisionResult.
func (d *SemselectDecider) DecidePassage(ctx context.Context, question string, sp ScoredPassage) (ScoredPassage, DecisionResult, error) {
	if d == nil || strings.TrimSpace(d.BaseURL) == "" {
		return sp, DecisionResult{}, errors.New("intake: SemselectDecider BaseURL is empty")
	}
	unsureCut := d.UnsureThreshold
	if unsureCut <= 0 {
		unsureCut = DefaultDecisionUnsureThreshold
	}
	yesCut := d.YesThreshold
	if yesCut <= 0 {
		yesCut = DefaultDecisionYesThreshold
	}
	if unsureCut > yesCut {
		unsureCut = yesCut
	}

	state := FormatDecisionState(question, sp, d.MaxStateBytes)
	start := time.Now()
	pAddr, pRel, err := d.queryBoth(ctx, state)
	if err != nil {
		return sp, DecisionResult{}, fmt.Errorf("intake: semselect decision: %w", err)
	}
	durMs := time.Since(start).Milliseconds()

	res := ClassifyDecisionProbabilities(pAddr, pRel, unsureCut, yesCut, durMs)
	sp.Decision = &res
	if res.Flagged {
		sp.Flagged = true
		switch res.Outcome {
		case DecisionYes:
			if sp.WeightMult > DefaultDecisionYesWeightMult {
				sp.WeightMult = DefaultDecisionYesWeightMult
			}
		case DecisionUnsure:
			if sp.WeightMult > DefaultDecisionUnsureWeightMult {
				sp.WeightMult = DefaultDecisionUnsureWeightMult
			}
		}
	}
	return sp, res, nil
}

// ClassifyDecisionProbabilities maps the two Noul probabilities to DecisionNo, DecisionUnsure, or DecisionYes.
func ClassifyDecisionProbabilities(pAddressed, pReliability, unsureCut, yesCut float64, durMs int64) DecisionResult {
	if unsureCut <= 0 {
		unsureCut = DefaultDecisionUnsureThreshold
	}
	if yesCut <= 0 {
		yesCut = DefaultDecisionYesThreshold
	}
	maxP := pAddressed
	dominant := QuestionIDAddressedToReader
	if pReliability > maxP {
		maxP = pReliability
		dominant = QuestionIDReliabilityOrRanking
	}

	var outcome DecisionOutcome
	var flagged bool
	var note string
	switch {
	case maxP >= yesCut:
		outcome = DecisionYes
		flagged = true
		note = fmt.Sprintf("flagged (%s=%.4f >= %.2f)", dominant, maxP, yesCut)
	case maxP >= unsureCut:
		outcome = DecisionUnsure
		flagged = true
		note = fmt.Sprintf("flagged unsure (%s=%.4f in [%.2f, %.2f))", dominant, maxP, unsureCut, yesCut)
	default:
		outcome = DecisionNo
		flagged = false
		note = fmt.Sprintf("kept (%s=%.4f < %.2f)", dominant, maxP, unsureCut)
	}

	return DecisionResult{
		Outcome:                  outcome,
		Flagged:                  flagged,
		Note:                     note,
		AddressedToReaderProb:    pAddressed,
		ReliabilityOrRankingProb: pReliability,
		MaxProb:                  maxP,
		YesAt50:                  maxP >= DefaultDecisionYesThreshold,
		DurationMs:               durMs,
	}
}

// DecidePassages evaluates every passage in res sequentially against d, updating
// res.Reports and res.Passages in place without deleting any passage.
func (d *SemselectDecider) DecidePassages(ctx context.Context, res *Result) error {
	if res == nil {
		return errors.New("intake: Result is nil")
	}
	flatIdx := 0
	for rIdx := range res.Reports {
		for pIdx := range res.Reports[rIdx].Passages {
			updated, _, err := d.DecidePassage(ctx, res.Question, res.Reports[rIdx].Passages[pIdx])
			if err != nil {
				return err
			}
			res.Reports[rIdx].Passages[pIdx] = updated
			if flatIdx < len(res.Passages) {
				res.Passages[flatIdx] = updated
			}
			flatIdx++
		}
	}
	return nil
}

func (d *SemselectDecider) queryBoth(ctx context.Context, state string) (pAddr, pRel float64, err error) {
	reqPayload := semselectRequest{
		Model: strings.TrimSpace(d.Model),
		State: state,
		Questions: map[string]semselectQuestion{
			QuestionIDAddressedToReader: {
				Type:         "noul",
				Instructions: QuestionAddressedToReader,
			},
			QuestionIDReliabilityOrRanking: {
				Type:         "noul",
				Instructions: QuestionReliabilityOrRanking,
			},
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(reqPayload); err != nil {
		return 0, 0, fmt.Errorf("marshal request: %w", err)
	}
	bodyBytes := bytes.TrimRight(buf.Bytes(), "\n")
	maxBytes := d.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = DefaultDecisionMaxBodyBytes
	}
	if int64(len(bodyBytes)) > maxBytes {
		return 0, 0, fmt.Errorf("request body %d bytes exceeds max %d", len(bodyBytes), maxBytes)
	}

	endpoints := semselectEndpoints(d.BaseURL)
	hc := d.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}

	endpointIdx := 0
	const maxAttempts = 4
	for attempt := 0; attempt < maxAttempts; attempt++ {
		endpoint := endpoints[endpointIdx]
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return 0, 0, fmt.Errorf("build request: %w", reqErr)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if d.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+d.APIKey)
		}

		httpResp, doErr := hc.Do(httpReq)
		if doErr != nil {
			return 0, 0, fmt.Errorf("transport: %w", doErr)
		}

		respBytes, readErr := io.ReadAll(io.LimitReader(httpResp.Body, maxBytes+1))
		_ = httpResp.Body.Close()
		if readErr != nil {
			return 0, 0, fmt.Errorf("read response: %w", readErr)
		}
		if int64(len(respBytes)) > maxBytes {
			return 0, 0, fmt.Errorf("response body exceeds max %d bytes", maxBytes)
		}

		if httpResp.StatusCode == http.StatusNotFound && endpointIdx+1 < len(endpoints) {
			endpointIdx++
			continue
		}

		if httpResp.StatusCode == http.StatusTooManyRequests && attempt+1 < maxAttempts {
			wait := parseRetryAfterHeader(httpResp.Header.Get("Retry-After"))
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return 0, 0, fmt.Errorf("transport: %w", ctx.Err())
			case <-timer.C:
				continue
			}
		}

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			return 0, 0, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBytes)))
		}

		var wresp semselectResponse
		if err := json.Unmarshal(respBytes, &wresp); err != nil {
			return 0, 0, fmt.Errorf("decode response: %w", err)
		}
		pA, err := validateDecisionNoul(QuestionIDAddressedToReader, wresp.Answers[QuestionIDAddressedToReader])
		if err != nil {
			return 0, 0, err
		}
		pR, err := validateDecisionNoul(QuestionIDReliabilityOrRanking, wresp.Answers[QuestionIDReliabilityOrRanking])
		if err != nil {
			return 0, 0, err
		}
		return pA, pR, nil
	}
	return 0, 0, errors.New("exhausted 429 retries")
}

func semselectEndpoints(baseURL string) []string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/v1/systemone") || strings.HasSuffix(base, "/v1/decisions") {
		return []string{base}
	}
	if strings.HasSuffix(base, "/v1") {
		return []string{base + "/systemone", base + "/decisions"}
	}
	return []string{base + "/v1/systemone", base + "/v1/decisions"}
}

func parseRetryAfterHeader(hdr string) time.Duration {
	if s := strings.TrimSpace(hdr); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
			if secs > 5 {
				secs = 5
			}
			return time.Duration(secs) * time.Second
		}
	}
	return 100 * time.Millisecond
}

func validateDecisionNoul(id string, ans semselectAnswer) (float64, error) {
	if ans.Type != "" && ans.Type != "noul" {
		return 0, fmt.Errorf("answer %q: unexpected type %q, want \"noul\"", id, ans.Type)
	}
	if ans.Noul == nil {
		return 0, fmt.Errorf("answer %q: missing noul probability", id)
	}
	p := *ans.Noul
	if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
		return 0, fmt.Errorf("answer %q: noul probability %v outside [0, 1]", id, p)
	}
	return p, nil
}
