package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// AnswerCheckName identifies one of the deterministic checks specific to a Perplexity answer.
type AnswerCheckName string

// AnswerCheckName values for the 4 Perplexity answer-specific checks.
const (
	// AnswerCheckUnlistedURL flags a URL in the answer text that is not in search_results.
	AnswerCheckUnlistedURL AnswerCheckName = "answer_unlisted_url"
	// AnswerCheckUnsourcedSentence flags a substantive answer sentence that maps to no source.
	AnswerCheckUnsourcedSentence AnswerCheckName = "unsourced_sentence"
	// AnswerCheckUnfetchedCitation flags a cited source whose page could not be fetched through Exa /contents.
	AnswerCheckUnfetchedCitation AnswerCheckName = "unfetched_citation"
	// AnswerCheckDanglingMarker flags an inline [n] marker or annotation pointing to no search_results entry.
	AnswerCheckDanglingMarker AnswerCheckName = "dangling_citation_marker"
)

// AllAnswerChecks lists the 4 Perplexity answer-specific checks in canonical evaluation order.
var AllAnswerChecks = []AnswerCheckName{
	AnswerCheckUnlistedURL,
	AnswerCheckUnsourcedSentence,
	AnswerCheckUnfetchedCitation,
	AnswerCheckDanglingMarker,
}

// AnswerFinding records one answer-level finding on a Perplexity Agent response.
type AnswerFinding struct {
	Check  AnswerCheckName `json:"check"`
	Detail string          `json:"detail"`
	URL    string          `json:"url,omitempty"`
	Marker string          `json:"marker,omitempty"`
	Span   *[2]int         `json:"span,omitempty"`
}

// PerplexitySearchResult is one search result from a Perplexity POST /v1/agent search_results output item.
type PerplexitySearchResult struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	CanonicalURL string `json:"canonical_url"`
	Site         string `json:"site"`
	Snippet      string `json:"snippet"`
	Date         string `json:"date,omitempty"`
	LastUpdated  string `json:"last_updated,omitempty"`
	Source       string `json:"source,omitempty"`
}

// PerplexityAnnotation is a url_citation span on a Perplexity POST /v1/agent message content part.
type PerplexityAnnotation struct {
	Type         string `json:"type"`
	StartIndex   int    `json:"start_index"`
	EndIndex     int    `json:"end_index"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	CanonicalURL string `json:"canonical_url"`
}

// PerplexityCost holds the Perplexity Agent API cost breakdown.
type PerplexityCost struct {
	Currency          string  `json:"currency"`
	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	CacheCreationCost float64 `json:"cache_creation_cost,omitempty"`
	CacheReadCost     float64 `json:"cache_read_cost,omitempty"`
	ToolCallsCost     float64 `json:"tool_calls_cost,omitempty"`
	TotalCost         float64 `json:"total_cost"`
}

// PerplexityUsage holds the Perplexity Agent API token and cost accounting.
type PerplexityUsage struct {
	InputTokens  int             `json:"input_tokens"`
	OutputTokens int             `json:"output_tokens"`
	TotalTokens  int             `json:"total_tokens"`
	Cost         *PerplexityCost `json:"cost,omitempty"`
}

// PerplexityErrorInfo holds the Perplexity Agent API error payload.
type PerplexityErrorInfo struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
}

// PerplexityResponse is the parsed Perplexity POST /v1/agent response.
type PerplexityResponse struct {
	ID            string                   `json:"id"`
	Object        string                   `json:"object"`
	Model         string                   `json:"model"`
	CreatedAt     int64                    `json:"created_at"`
	Status        string                   `json:"status"`
	Queries       []string                 `json:"queries,omitempty"`
	SearchResults []PerplexitySearchResult `json:"search_results"`
	MessageID     string                   `json:"message_id,omitempty"`
	MessageStatus string                   `json:"message_status,omitempty"`
	Answer        string                   `json:"answer"`
	Annotations   []PerplexityAnnotation   `json:"annotations"`
	Error         *PerplexityErrorInfo     `json:"error,omitempty"`
	Usage         *PerplexityUsage         `json:"usage,omitempty"`
}

// SentenceCitation maps one sentence in the Perplexity answer to its resolved search_results IDs and URLs.
type SentenceCitation struct {
	Index           int      `json:"index"`
	Start           int      `json:"start"`
	End             int      `json:"end"`
	Text            string   `json:"text"`
	SearchResultIDs []int    `json:"search_result_ids,omitempty"`
	URLs            []string `json:"urls,omitempty"`
}

// CitedPageStatus records whether a cited URL was resolved via Exa /contents.
type CitedPageStatus struct {
	URL            string `json:"url"`
	CanonicalURL   string `json:"canonical_url"`
	SearchResultID int    `json:"search_result_id,omitempty"`
	Fetched        bool   `json:"fetched"`
	Status         string `json:"status"`
	ErrorTag       string `json:"error_tag,omitempty"`
}

// PerplexityIntakeResult holds the complete result of running the Perplexity intake pipeline:
// the parsed Perplexity response, cleaned answer, sentence-to-source mapping, answer findings,
// cited page fetch statuses and share, and the Exa page-level intake Result.
type PerplexityIntakeResult struct {
	Question          string                  `json:"question"`
	Response          PerplexityResponse      `json:"response"`
	CleanedAnswer     Cleaned                 `json:"cleaned_answer"`
	SentenceCitations []SentenceCitation      `json:"sentence_citations"`
	AnswerFindings    []AnswerFinding         `json:"answer_findings,omitempty"`
	AnswerCheckCounts map[AnswerCheckName]int `json:"answer_check_counts"`
	CitedURLs         []string                `json:"cited_urls"`
	CitedPageStatuses []CitedPageStatus       `json:"cited_page_statuses"`
	CitedCount        int                     `json:"cited_count"`
	FetchedCount      int                     `json:"fetched_count"`
	UnfetchedCount    int                     `json:"unfetched_count"`
	UnfetchedShare    float64                 `json:"unfetched_share"`
	PageIntake        Result                  `json:"page_intake"`
}

// ExaContentsFetcher fetches raw Exa POST /contents JSON bytes for a slice of URLs.
type ExaContentsFetcher func(ctx context.Context, urls []string) ([]byte, error)

// NewHTTPExaContentsFetcher returns an ExaContentsFetcher that POSTs {"urls": urls, "text": true}
// to baseURL+"/contents" using client and apiKey. Responses larger than DefaultMaxParseBytes
// are rejected with ErrParseOversized before reading unbounded bytes into memory.
func NewHTTPExaContentsFetcher(client *http.Client, baseURL, apiKey string) ExaContentsFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return func(ctx context.Context, urls []string) ([]byte, error) {
		reqBody, err := json.Marshal(struct {
			URLs []string `json:"urls"`
			Text bool     `json:"text"`
		}{
			URLs: urls,
			Text: true,
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, trimmedBase+"/contents", bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(resp.Body, int64(DefaultMaxParseBytes)+1))
		if err != nil {
			return nil, err
		}
		if len(body) > DefaultMaxParseBytes {
			return nil, fmt.Errorf("%w: %d > %d", ErrParseOversized, len(body), DefaultMaxParseBytes)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("intake: Exa /contents status %d: %s", resp.StatusCode, truncateDetail(strings.TrimSpace(string(body))))
		}
		return body, nil
	}
}

// Wire decoding structs for Perplexity POST /v1/agent.
type perplexityAgentWire struct {
	ID        *string              `json:"id"`
	Object    *string              `json:"object"`
	Model     *string              `json:"model"`
	CreatedAt *int64               `json:"created_at"`
	Status    *string              `json:"status"`
	Output    *[]json.RawMessage   `json:"output"`
	Error     *PerplexityErrorInfo `json:"error,omitempty"`
	Usage     *perplexityUsageWire `json:"usage,omitempty"`
}

type perplexityUsageWire struct {
	InputTokens  *int            `json:"input_tokens"`
	OutputTokens *int            `json:"output_tokens"`
	TotalTokens  *int            `json:"total_tokens"`
	Cost         *PerplexityCost `json:"cost,omitempty"`
}

type perplexityOutputTypeProbe struct {
	Type *string `json:"type"`
}

type perplexitySearchResultsItemWire struct {
	Type    string                        `json:"type"`
	Queries []string                      `json:"queries,omitempty"`
	Results *[]perplexitySearchResultWire `json:"results"`
}

type perplexitySearchResultWire struct {
	ID          *int    `json:"id"`
	Title       *string `json:"title"`
	URL         *string `json:"url"`
	Snippet     *string `json:"snippet"`
	Date        *string `json:"date,omitempty"`
	LastUpdated *string `json:"last_updated,omitempty"`
	Source      string  `json:"source,omitempty"`
}

type perplexityMessageItemWire struct {
	Type    string                       `json:"type"`
	ID      *string                      `json:"id"`
	Role    *string                      `json:"role"`
	Status  *string                      `json:"status"`
	Content *[]perplexityContentPartWire `json:"content"`
}

type perplexityContentPartWire struct {
	Type        *string                     `json:"type"`
	Text        *string                     `json:"text"`
	Annotations *[]perplexityAnnotationWire `json:"annotations"`
}

type perplexityAnnotationWire struct {
	Type       *string `json:"type"`
	StartIndex *int    `json:"start_index"`
	EndIndex   *int    `json:"end_index"`
	Title      *string `json:"title"`
	URL        *string `json:"url"`
}

// ParsePerplexityAgent strictly decodes a Perplexity POST /v1/agent JSON response body.
func ParsePerplexityAgent(ctx context.Context, body []byte, opts ParseOptions) (PerplexityResponse, error) {
	if err := checkParsePreconditions(ctx, body, opts); err != nil {
		return PerplexityResponse{}, err
	}
	var w perplexityAgentWire
	if err := decodeStrictJSON(body, &w); err != nil {
		return PerplexityResponse{}, fmt.Errorf("%w: %v", ErrParseShape, err)
	}
	if w.ID == nil || strings.TrimSpace(*w.ID) == "" {
		return PerplexityResponse{}, fmt.Errorf("%w: missing id", ErrParseShape)
	}
	if w.Object == nil || *w.Object != "response" {
		return PerplexityResponse{}, fmt.Errorf("%w: missing or invalid object (want \"response\")", ErrParseShape)
	}
	if w.Model == nil || strings.TrimSpace(*w.Model) == "" {
		return PerplexityResponse{}, fmt.Errorf("%w: missing model", ErrParseShape)
	}
	if w.CreatedAt == nil {
		return PerplexityResponse{}, fmt.Errorf("%w: missing created_at", ErrParseShape)
	}
	if w.Status == nil || strings.TrimSpace(*w.Status) == "" {
		return PerplexityResponse{}, fmt.Errorf("%w: missing status", ErrParseShape)
	}
	if w.Output == nil {
		return PerplexityResponse{}, fmt.Errorf("%w: missing output array", ErrParseShape)
	}
	if w.Error != nil && strings.TrimSpace(w.Error.Message) == "" {
		return PerplexityResponse{}, fmt.Errorf("%w: error.message is empty", ErrParseShape)
	}

	out := PerplexityResponse{
		ID:            *w.ID,
		Object:        *w.Object,
		Model:         *w.Model,
		CreatedAt:     *w.CreatedAt,
		Status:        *w.Status,
		SearchResults: []PerplexitySearchResult{},
		Annotations:   []PerplexityAnnotation{},
		Error:         w.Error,
	}
	if w.Usage != nil {
		if w.Usage.InputTokens == nil || w.Usage.OutputTokens == nil || w.Usage.TotalTokens == nil {
			return PerplexityResponse{}, fmt.Errorf("%w: usage missing token counts", ErrParseShape)
		}
		out.Usage = &PerplexityUsage{
			InputTokens:  *w.Usage.InputTokens,
			OutputTokens: *w.Usage.OutputTokens,
			TotalTokens:  *w.Usage.TotalTokens,
			Cost:         w.Usage.Cost,
		}
	}

	for idx, rawItem := range *w.Output {
		var probe perplexityOutputTypeProbe
		if err := json.Unmarshal(rawItem, &probe); err != nil || probe.Type == nil {
			return PerplexityResponse{}, fmt.Errorf("%w: output[%d]: missing or invalid type", ErrParseShape, idx)
		}
		switch *probe.Type {
		case "search_results":
			var srItem perplexitySearchResultsItemWire
			if err := decodeStrictJSON(rawItem, &srItem); err != nil {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (search_results): %v", ErrParseShape, idx, err)
			}
			if srItem.Results == nil {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (search_results): missing results array", ErrParseShape, idx)
			}
			out.Queries = append(out.Queries, srItem.Queries...)
			for rIdx, rw := range *srItem.Results {
				if rw.ID == nil || *rw.ID <= 0 {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].results[%d]: missing or non-positive integer id", ErrParseShape, idx, rIdx)
				}
				if rw.Title == nil {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].results[%d]: missing title", ErrParseShape, idx, rIdx)
				}
				if rw.URL == nil || strings.TrimSpace(*rw.URL) == "" {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].results[%d]: missing url", ErrParseShape, idx, rIdx)
				}
				if rw.Snippet == nil {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].results[%d]: missing snippet", ErrParseShape, idx, rIdx)
				}
				rawURL := strings.TrimSpace(*rw.URL)
				canonURL, ok := CanonicalizeURL(rawURL)
				if !ok {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].results[%d]: invalid http/https url %q", ErrParseShape, idx, rIdx, rawURL)
				}
				var dateStr, lastUpdStr string
				if rw.Date != nil {
					dateStr = *rw.Date
				}
				if rw.LastUpdated != nil {
					lastUpdStr = *rw.LastUpdated
				}
				out.SearchResults = append(out.SearchResults, PerplexitySearchResult{
					ID:           *rw.ID,
					Title:        *rw.Title,
					URL:          rawURL,
					CanonicalURL: canonURL,
					Site:         SiteHost(canonURL),
					Snippet:      *rw.Snippet,
					Date:         dateStr,
					LastUpdated:  lastUpdStr,
					Source:       rw.Source,
				})
			}

		case "message":
			var msgItem perplexityMessageItemWire
			if err := decodeStrictJSON(rawItem, &msgItem); err != nil {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (message): %v", ErrParseShape, idx, err)
			}
			if msgItem.ID == nil || strings.TrimSpace(*msgItem.ID) == "" {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (message): missing id", ErrParseShape, idx)
			}
			if msgItem.Role == nil || strings.TrimSpace(*msgItem.Role) == "" {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (message): missing role", ErrParseShape, idx)
			}
			if msgItem.Status == nil || strings.TrimSpace(*msgItem.Status) == "" {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (message): missing status", ErrParseShape, idx)
			}
			if msgItem.Content == nil {
				return PerplexityResponse{}, fmt.Errorf("%w: output[%d] (message): missing content array", ErrParseShape, idx)
			}
			out.MessageID = *msgItem.ID
			out.MessageStatus = *msgItem.Status
			var textBuilder strings.Builder
			for cIdx, cp := range *msgItem.Content {
				if cp.Type == nil || *cp.Type != "output_text" {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].content[%d]: unexpected type", ErrParseShape, idx, cIdx)
				}
				if cp.Text == nil {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].content[%d]: missing text", ErrParseShape, idx, cIdx)
				}
				if cp.Annotations == nil {
					return PerplexityResponse{}, fmt.Errorf("%w: output[%d].content[%d]: missing annotations array", ErrParseShape, idx, cIdx)
				}
				baseOffset := textBuilder.Len()
				partText := *cp.Text
				textBuilder.WriteString(partText)
				for aIdx, aw := range *cp.Annotations {
					if aw.Type == nil || *aw.Type != "url_citation" {
						return PerplexityResponse{}, fmt.Errorf("%w: output[%d].content[%d].annotations[%d]: unexpected type", ErrParseShape, idx, cIdx, aIdx)
					}
					if aw.StartIndex == nil || aw.EndIndex == nil {
						return PerplexityResponse{}, fmt.Errorf("%w: output[%d].content[%d].annotations[%d]: missing start_index or end_index", ErrParseShape, idx, cIdx, aIdx)
					}
					if aw.Title == nil || aw.URL == nil || strings.TrimSpace(*aw.URL) == "" {
						return PerplexityResponse{}, fmt.Errorf("%w: output[%d].content[%d].annotations[%d]: missing title or url", ErrParseShape, idx, cIdx, aIdx)
					}
					rawAnnURL := strings.TrimSpace(*aw.URL)
					canonAnnURL, ok := CanonicalizeURL(rawAnnURL)
					if !ok {
						canonAnnURL = rawAnnURL
					}
					out.Annotations = append(out.Annotations, PerplexityAnnotation{
						Type:         *aw.Type,
						StartIndex:   baseOffset + *aw.StartIndex,
						EndIndex:     baseOffset + *aw.EndIndex,
						Title:        *aw.Title,
						URL:          rawAnnURL,
						CanonicalURL: canonAnnURL,
					})
				}
			}
			out.Answer = textBuilder.String()

		default:
			return PerplexityResponse{}, fmt.Errorf("%w: output[%d]: unknown item type %q", ErrParseShape, idx, *probe.Type)
		}
	}

	return out, nil
}

var inlineCitationMarkerRe = regexp.MustCompile(`\[(\d+)\]`)

// ProcessPerplexityAgent parses a Perplexity POST /v1/agent response, maps the answer to its sources,
// fetches cited pages via Exa POST /contents using fetcher, runs page-level and answer-level checks,
// and returns the combined PerplexityIntakeResult. When fetcher is nil, no /contents request is made
// and any cited URLs are reported as unfetched_citation.
func ProcessPerplexityAgent(
	ctx context.Context,
	question string,
	body []byte,
	fetcher ExaContentsFetcher,
	parseOpts ParseOptions,
	cleanOpts CleanOptions,
	scoreOpts ScoreOptions,
) (PerplexityIntakeResult, error) {
	pplx, err := ParsePerplexityAgent(ctx, body, parseOpts)
	if err != nil {
		return PerplexityIntakeResult{}, err
	}
	citedURLs := collectCitedURLs(pplx)
	var exaContents ExaContentsResponse
	if len(citedURLs) > 0 && fetcher != nil {
		contentsBytes, fetchErr := fetcher(ctx, citedURLs)
		if fetchErr != nil {
			return PerplexityIntakeResult{}, fetchErr
		}
		exaContents, err = ParseExaContents(ctx, contentsBytes, parseOpts)
		if err != nil {
			return PerplexityIntakeResult{}, err
		}
	}
	return ResolvePerplexityWithContents(ctx, question, pplx, exaContents, cleanOpts, scoreOpts)
}

// ResolvePerplexityWithContents runs the Perplexity answer mapping, answer checks, and Exa page-level checks
// using a parsed PerplexityResponse and a parsed ExaContentsResponse.
func ResolvePerplexityWithContents(
	ctx context.Context,
	question string,
	pplx PerplexityResponse,
	contents ExaContentsResponse,
	cleanOpts CleanOptions,
	scoreOpts ScoreOptions,
) (PerplexityIntakeResult, error) {
	cleanedAnswer, err := Clean(ctx, pplx.Answer, cleanOpts)
	if err != nil {
		return PerplexityIntakeResult{}, fmt.Errorf("clean perplexity answer: %w", err)
	}

	// Index search_results by integer ID and by canonical URL.
	byID := make(map[int]PerplexitySearchResult, len(pplx.SearchResults))
	byURL := make(map[string]PerplexitySearchResult, len(pplx.SearchResults))
	for _, sr := range pplx.SearchResults {
		byID[sr.ID] = sr
		if sr.CanonicalURL != "" {
			byURL[sr.CanonicalURL] = sr
		}
	}

	answerCheckCounts := make(map[AnswerCheckName]int, len(AllAnswerChecks))
	for _, c := range AllAnswerChecks {
		answerCheckCounts[c] = 0
	}
	var findings []AnswerFinding
	recordFinding := func(f AnswerFinding) {
		findings = append(findings, f)
		answerCheckCounts[f.Check]++
	}

	// 1. Validate annotations against answer length and search_results.
	// shortcut: annotation start_index/end_index are treated as UTF-8 byte offsets matching servicesim; verify byte vs rune/UTF-16 indexing on non-ASCII responses in #55.
	type validAnnotation struct {
		ann      PerplexityAnnotation
		resultID int
		canonURL string
	}
	var validAnns []validAnnotation
	for _, ann := range pplx.Annotations {
		if ann.StartIndex < 0 || ann.EndIndex <= ann.StartIndex || ann.EndIndex > len(pplx.Answer) {
			span := [2]int{ann.StartIndex, ann.EndIndex}
			recordFinding(AnswerFinding{
				Check:  AnswerCheckDanglingMarker,
				Detail: fmt.Sprintf("annotation span [%d, %d) out of bounds for answer length %d", ann.StartIndex, ann.EndIndex, len(pplx.Answer)),
				URL:    ann.URL,
				Span:   &span,
			})
			continue
		}
		sr, ok := byURL[ann.CanonicalURL]
		if !ok {
			span := [2]int{ann.StartIndex, ann.EndIndex}
			recordFinding(AnswerFinding{
				Check:  AnswerCheckDanglingMarker,
				Detail: fmt.Sprintf("annotation URL %q does not match any search_results entry", ann.URL),
				URL:    ann.URL,
				Span:   &span,
			})
			continue
		}
		validAnns = append(validAnns, validAnnotation{
			ann:      ann,
			resultID: sr.ID,
			canonURL: sr.CanonicalURL,
		})
	}

	// 2. Scan raw answer for inline [n] citation markers and validate against search_results.
	type inlineMarker struct {
		start    int
		end      int
		raw      string
		id       int
		valid    bool
		canonURL string
	}
	var inlineMarkers []inlineMarker
	for _, loc := range inlineCitationMarkerRe.FindAllStringSubmatchIndex(pplx.Answer, -1) {
		fullStart, fullEnd := loc[0], loc[1]
		rawTok := pplx.Answer[fullStart:fullEnd]
		numStr := pplx.Answer[loc[2]:loc[3]]
		n, convErr := strconv.Atoi(numStr)
		if convErr != nil {
			span := [2]int{fullStart, fullEnd}
			recordFinding(AnswerFinding{
				Check:  AnswerCheckDanglingMarker,
				Detail: fmt.Sprintf("inline citation marker %s points to no search_results entry", rawTok),
				Marker: rawTok,
				Span:   &span,
			})
			inlineMarkers = append(inlineMarkers, inlineMarker{
				start: fullStart, end: fullEnd, raw: rawTok, valid: false,
			})
			continue
		}
		sr, ok := byID[n]
		if !ok {
			span := [2]int{fullStart, fullEnd}
			recordFinding(AnswerFinding{
				Check:  AnswerCheckDanglingMarker,
				Detail: fmt.Sprintf("inline citation marker %s points to no search_results entry", rawTok),
				Marker: rawTok,
				Span:   &span,
			})
			inlineMarkers = append(inlineMarkers, inlineMarker{
				start: fullStart, end: fullEnd, raw: rawTok, id: n, valid: false,
			})
			continue
		}
		inlineMarkers = append(inlineMarkers, inlineMarker{
			start: fullStart, end: fullEnd, raw: rawTok, id: n, valid: true, canonURL: sr.CanonicalURL,
		})
	}

	// 3. Check 1 on answer: any URL in the answer text that is not in search_results.
	for _, u := range cleanedAnswer.URLs {
		if _, ok := byURL[u]; !ok {
			recordFinding(AnswerFinding{
				Check:  AnswerCheckUnlistedURL,
				Detail: fmt.Sprintf("answer URL %q is absent from search_results", u),
				URL:    u,
			})
		}
	}

	// 4. Split raw answer into sentences and map each sentence to sources via annotations (when present)
	// or inline [n] markers (when annotations are absent, or when inline markers appear inside the sentence).
	rawSentences := splitAnswerSentences(pplx.Answer)
	sentenceCitations := make([]SentenceCitation, 0, len(rawSentences))
	for sIdx, sent := range rawSentences {
		var ids []int
		var urls []string
		addSource := func(id int, u string) {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
			if u != "" && !slices.Contains(urls, u) {
				urls = append(urls, u)
			}
		}

		if len(pplx.Annotations) > 0 {
			for _, va := range validAnns {
				// Annotation overlaps sentence [sent.start, sent.end) or sits immediately at the sentence boundary.
				if va.ann.StartIndex < sent.end && va.ann.EndIndex > sent.start {
					addSource(va.resultID, va.canonURL)
				}
			}
		} else {
			for _, im := range inlineMarkers {
				if im.valid && im.start >= sent.start && im.end <= sent.end {
					addSource(im.id, im.canonURL)
				}
			}
		}

		sc := SentenceCitation{
			Index:           sIdx,
			Start:           sent.start,
			End:             sent.end,
			Text:            sent.text,
			SearchResultIDs: ids,
			URLs:            urls,
		}
		sentenceCitations = append(sentenceCitations, sc)

		// Check 2 on answer: a substantive sentence (>= 5 content tokens) with no source at all.
		if len(ids) == 0 && isSubstantiveSentence(sent.text) {
			span := [2]int{sent.start, sent.end}
			recordFinding(AnswerFinding{
				Check:  AnswerCheckUnsourcedSentence,
				Detail: fmt.Sprintf("sentence %d has no resolved source: %q", sIdx+1, truncateDetail(sent.text)),
				Span:   &span,
			})
		}
	}

	// 5. Determine cited URLs and reconcile with Exa /contents results and statuses.
	citedURLs := collectCitedURLs(pplx)
	exaResultByCanon := make(map[string]ExaResult, len(contents.Results))
	for _, r := range contents.Results {
		if r.CanonicalURL != "" {
			exaResultByCanon[r.CanonicalURL] = r
		}
	}
	exaStatusByCanon := make(map[string]ExaContentsStatus, len(contents.Statuses))
	for _, st := range contents.Statuses {
		if c, ok := CanonicalizeURL(st.ID); ok {
			exaStatusByCanon[c] = st
		} else {
			exaStatusByCanon[strings.TrimSpace(st.ID)] = st
		}
	}

	var resolvedExaResults []ExaResult
	citedStatuses := make([]CitedPageStatus, 0, len(citedURLs))
	fetchedCount := 0
	unfetchedCount := 0

	for _, rawCited := range citedURLs {
		canonCited, ok := CanonicalizeURL(rawCited)
		if !ok {
			canonCited = rawCited
		}
		sr := byURL[canonCited]
		st, hasStatus := exaStatusByCanon[canonCited]
		exaRes, hasRes := exaResultByCanon[canonCited]

		if hasRes && strings.TrimSpace(exaRes.Text) != "" && (!hasStatus || st.Status == "success") {
			fetchedCount++
			citedStatuses = append(citedStatuses, CitedPageStatus{
				URL:            rawCited,
				CanonicalURL:   canonCited,
				SearchResultID: sr.ID,
				Fetched:        true,
				Status:         "success",
			})
			resolvedExaResults = append(resolvedExaResults, exaRes)
			continue
		}

		unfetchedCount++
		statusLabel := "unfetched"
		errTag := ""
		if hasStatus {
			statusLabel = st.Status
			if st.Error != nil {
				errTag = st.Error.Tag
			}
		}
		citedStatuses = append(citedStatuses, CitedPageStatus{
			URL:            rawCited,
			CanonicalURL:   canonCited,
			SearchResultID: sr.ID,
			Fetched:        false,
			Status:         statusLabel,
			ErrorTag:       errTag,
		})
		detail := fmt.Sprintf("cited page %q could not be fetched (status=%s)", rawCited, statusLabel)
		if errTag != "" {
			detail = fmt.Sprintf("cited page %q could not be fetched (status=%s, tag=%s)", rawCited, statusLabel, errTag)
		}
		recordFinding(AnswerFinding{
			Check:  AnswerCheckUnfetchedCitation,
			Detail: detail,
			URL:    rawCited,
		})
	}

	var unfetchedShare float64
	if len(citedURLs) > 0 {
		unfetchedShare = float64(unfetchedCount) / float64(len(citedURLs))
	}

	// 6. Run Clean, Split, BuildGraph, and the 8 page-level checks on the fetched Exa pages.
	reports, err := prepareExaReportsWithContext(ctx, resolvedExaResults, cleanOpts)
	if err != nil {
		return PerplexityIntakeResult{}, err
	}
	pageIntake := CheckAndScore(question, "perplexity", reports, scoreOpts)

	return PerplexityIntakeResult{
		Question:          question,
		Response:          pplx,
		CleanedAnswer:     cleanedAnswer,
		SentenceCitations: sentenceCitations,
		AnswerFindings:    findings,
		AnswerCheckCounts: answerCheckCounts,
		CitedURLs:         citedURLs,
		CitedPageStatuses: citedStatuses,
		CitedCount:        len(citedURLs),
		FetchedCount:      fetchedCount,
		UnfetchedCount:    unfetchedCount,
		UnfetchedShare:    unfetchedShare,
		PageIntake:        pageIntake,
	}, nil
}

// collectCitedURLs returns the ordered, deduplicated list of search_results URLs cited
// by the answer via valid in-bounds annotations or valid inline [n] markers.
// Uncited search_results entries are not included.
func collectCitedURLs(pplx PerplexityResponse) []string {
	byID := make(map[int]PerplexitySearchResult, len(pplx.SearchResults))
	byCanon := make(map[string]PerplexitySearchResult, len(pplx.SearchResults))
	for _, sr := range pplx.SearchResults {
		byID[sr.ID] = sr
		if sr.CanonicalURL != "" {
			byCanon[sr.CanonicalURL] = sr
		}
	}

	var out []string
	seenCanon := make(map[string]bool)
	add := func(sr PerplexitySearchResult) {
		if sr.CanonicalURL == "" || seenCanon[sr.CanonicalURL] {
			return
		}
		seenCanon[sr.CanonicalURL] = true
		out = append(out, sr.URL)
	}

	for _, ann := range pplx.Annotations {
		if ann.StartIndex < 0 || ann.EndIndex <= ann.StartIndex || ann.EndIndex > len(pplx.Answer) {
			continue
		}
		if sr, ok := byCanon[ann.CanonicalURL]; ok {
			add(sr)
		}
	}
	for _, m := range inlineCitationMarkerRe.FindAllStringSubmatch(pplx.Answer, -1) {
		if id, err := strconv.Atoi(m[1]); err == nil {
			if sr, ok := byID[id]; ok {
				add(sr)
			}
		}
	}
	return out
}

type answerSentence struct {
	start int
	end   int
	text  string
}

func splitAnswerSentences(text string) []answerSentence {
	var out []answerSentence
	n := len(text)
	start := 0

	flush := func(end int) {
		for start < end && (text[start] == ' ' || text[start] == '\t' || text[start] == '\n' || text[start] == '\r') {
			start++
		}
		trimmedEnd := end
		for trimmedEnd > start && (text[trimmedEnd-1] == ' ' || text[trimmedEnd-1] == '\t' || text[trimmedEnd-1] == '\n' || text[trimmedEnd-1] == '\r') {
			trimmedEnd--
		}
		if trimmedEnd > start {
			out = append(out, answerSentence{
				start: start,
				end:   trimmedEnd,
				text:  text[start:trimmedEnd],
			})
		}
		start = end
	}

	for i := 0; i < n; i++ {
		switch text[i] {
		case '\n':
			flush(i)
			start = i + 1
		case '.', '!', '?':
			// Avoid splitting inside URLs (e.g. "https://example.com/a.html").
			next := i + 1
			// Include any trailing inline citation markers like "sentence.[1]" or "sentence [1]."
			for next < n && text[next] == '[' {
				closeIdx := strings.IndexByte(text[next:], ']')
				if closeIdx <= 1 {
					break
				}
				allDigits := true
				for k := next + 1; k < next+closeIdx; k++ {
					if text[k] < '0' || text[k] > '9' {
						allDigits = false
						break
					}
				}
				if !allDigits {
					break
				}
				next += closeIdx + 1
				if next < n && (text[next] == '.' || text[next] == '!' || text[next] == '?') {
					next++
				}
			}
			if next == n || text[next] == ' ' || text[next] == '\t' || text[next] == '\n' || text[next] == '\r' {
				flush(next)
				i = next - 1
			}
		}
	}
	if start < n {
		flush(n)
	}
	return out
}

func isSubstantiveSentence(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	// Strip inline [n] markers before counting content tokens.
	withoutMarkers := inlineCitationMarkerRe.ReplaceAllString(trimmed, " ")
	return len(extractContentTokens(withoutMarkers)) >= 5
}
