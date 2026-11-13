package intake

import "context"

// Band represents a passage's provisional suspicion band.
type Band string

const (
	// BandLow is the low-suspicion band (score < MiddleCutoff).
	BandLow Band = "low"
	// BandMiddle is the middle-suspicion band (MiddleCutoff <= score < HighCutoff; candidates for #54).
	BandMiddle Band = "middle"
	// BandHigh is the high-suspicion band (score >= HighCutoff).
	BandHigh Band = "high"
)

// Provisional band score cut-offs (tuned once in #53 under the stop rule).
const (
	DefaultMiddleCutoff = 2
	DefaultHighCutoff   = 5
)

// DefaultWeights defines the provisional integer weights for each of the 8 checks.
// High-signal direct injection checks (talks_to_reader, fake_markers, encoded_blob) carry weight 3,
// self_rank_claim and unlisted_link carry weight 2, and contextual/graph signals (lone_site,
// shared_origin, off_topic) carry weight 1.
var DefaultWeights = map[CheckName]int{
	CheckTalksToReader: 3,
	CheckSelfRankClaim: 2,
	CheckFakeMarkers:   3,
	CheckEncodedBlob:   3,
	CheckUnlistedLink:  2,
	CheckLoneSite:      1,
	CheckSharedOrigin:  1,
	CheckOffTopic:      1,
}

// ScoreOptions configures passage scoring and band cut-offs.
type ScoreOptions struct {
	// Weights overrides DefaultWeights for any check present in the map.
	Weights map[CheckName]int
	// MiddleCutoff is the minimum score for BandMiddle (defaults to DefaultMiddleCutoff = 2).
	MiddleCutoff int
	// HighCutoff is the minimum score for BandHigh (defaults to DefaultHighCutoff = 5).
	HighCutoff int
}

// ScoredPassage wraps a Passage with its triggered checks, weighted score, band,
// down-weight factor, and optional decision-model outcome (#54).
// Flagged passages are down-weighted and reported, never deleted.
type ScoredPassage struct {
	Passage    Passage         `json:"passage"`
	ReportID   string          `json:"report_id"`
	ReportURL  string          `json:"report_url"`
	Site       string          `json:"site"`
	Hits       []CheckHit      `json:"hits,omitempty"`
	Checks     []CheckName     `json:"checks,omitempty"`
	Score      int             `json:"score"`
	Band       Band            `json:"band"`
	Flagged    bool            `json:"flagged"`
	WeightMult float64         `json:"weight_mult"`
	Decision   *DecisionResult `json:"decision,omitempty"`
}

// ScoredReport holds a Report along with its ScoredPassages.
type ScoredReport struct {
	Report   Report          `json:"report"`
	Passages []ScoredPassage `json:"passages"`
}

// Result is the complete output of an intake pipeline run:
// the prepared and scored reports, flattened scored passages, in-memory graph,
// and per-check / per-band summary counts.
type Result struct {
	Question    string            `json:"question"`
	Provider    string            `json:"provider"`
	Reports     []ScoredReport    `json:"reports"`
	Passages    []ScoredPassage   `json:"passages"`
	Graph       *Graph            `json:"graph"`
	CheckCounts map[CheckName]int `json:"check_counts"`
	BandCounts  map[Band]int      `json:"band_counts"`
}

// CheckAndScore runs all 8 deterministic passage checks across the prepared reports,
// builds the in-memory Graph, and computes each passage's provisional score, band,
// and down-weight multiplier without deleting any passage.
func CheckAndScore(question, providerName string, reports []Report, opts ScoreOptions) Result {
	midCutoff := opts.MiddleCutoff
	if midCutoff <= 0 {
		midCutoff = DefaultMiddleCutoff
	}
	highCutoff := opts.HighCutoff
	if highCutoff <= 0 {
		highCutoff = DefaultHighCutoff
	}
	weightFor := func(c CheckName) int {
		if opts.Weights != nil {
			if w, ok := opts.Weights[c]; ok {
				return w
			}
		}
		return DefaultWeights[c]
	}

	graph := BuildGraph(question, providerName, reports)
	bc := buildBatchContext(question, reports)

	checkCounts := make(map[CheckName]int, len(AllChecks))
	for _, c := range AllChecks {
		checkCounts[c] = 0
	}
	bandCounts := map[Band]int{
		BandLow:    0,
		BandMiddle: 0,
		BandHigh:   0,
	}

	scoredReports := make([]ScoredReport, len(reports))
	var allPassages []ScoredPassage

	for repIdx, rep := range reports {
		spList := make([]ScoredPassage, len(rep.Passages))
		for psgIdx, psg := range rep.Passages {
			var hits []CheckHit
			var names []CheckName
			score := 0

			recordHit := func(c CheckName, fired bool, detail string) {
				if !fired {
					return
				}
				w := weightFor(c)
				hits = append(hits, CheckHit{Check: c, Weight: w, Detail: detail})
				names = append(names, c)
				score += w
				checkCounts[c]++
			}

			f1, d1 := checkTalksToReader(psg.Body)
			recordHit(CheckTalksToReader, f1, d1)

			f2, d2 := checkSelfRankClaim(psg.Body)
			recordHit(CheckSelfRankClaim, f2, d2)

			f3, d3 := checkFakeMarkers(psg.Body)
			recordHit(CheckFakeMarkers, f3, d3)

			f4, d4 := checkEncodedBlob(psg.Body)
			recordHit(CheckEncodedBlob, f4, d4)

			f5, d5 := checkUnlistedLink(psg.Body, rep)
			recordHit(CheckUnlistedLink, f5, d5)

			f6, d6 := checkLoneSite(psg.Body, repIdx, rep, len(reports), bc)
			recordHit(CheckLoneSite, f6, d6)

			f7, d7 := checkSharedOrigin(psg.Body, rep, bc)
			recordHit(CheckSharedOrigin, f7, d7)

			f8, d8 := checkOffTopic(psgIdx, psg.Body, rep, bc)
			recordHit(CheckOffTopic, f8, d8)

			band := BandForScore(score, midCutoff, highCutoff)
			bandCounts[band]++
			flagged := len(hits) > 0

			sp := ScoredPassage{
				Passage:    psg,
				ReportID:   rep.ID,
				ReportURL:  rep.CanonicalURL,
				Site:       rep.Site,
				Hits:       hits,
				Checks:     names,
				Score:      score,
				Band:       band,
				Flagged:    flagged,
				WeightMult: weightMultiplier(band, flagged),
			}
			spList[psgIdx] = sp
			allPassages = append(allPassages, sp)
		}
		scoredReports[repIdx] = ScoredReport{
			Report:   rep,
			Passages: spList,
		}
	}

	return Result{
		Question:    question,
		Provider:    providerName,
		Reports:     scoredReports,
		Passages:    allPassages,
		Graph:       graph,
		CheckCounts: checkCounts,
		BandCounts:  bandCounts,
	}
}

// BandForScore maps an integer score to BandLow, BandMiddle, or BandHigh.
func BandForScore(score, middleCutoff, highCutoff int) Band {
	if middleCutoff <= 0 {
		middleCutoff = DefaultMiddleCutoff
	}
	if highCutoff <= 0 {
		highCutoff = DefaultHighCutoff
	}
	switch {
	case score >= highCutoff:
		return BandHigh
	case score >= middleCutoff:
		return BandMiddle
	default:
		return BandLow
	}
}

func weightMultiplier(band Band, flagged bool) float64 {
	switch band {
	case BandHigh:
		return 0.1
	case BandMiddle:
		return 0.5
	default:
		if flagged {
			return 0.8
		}
		return 1.0
	}
}

// ProcessExaSearch runs the full Exa intake pipeline (ParseExaSearch -> Clean -> Split -> Graph -> Checks -> Score)
// on a raw POST /search response body.
func ProcessExaSearch(ctx context.Context, question string, body []byte, parseOpts ParseOptions, cleanOpts CleanOptions, scoreOpts ScoreOptions) (ExaSearchResponse, Result, error) {
	parsed, err := ParseExaSearch(ctx, body, parseOpts)
	if err != nil {
		return ExaSearchResponse{}, Result{}, err
	}
	reports, err := prepareExaReportsWithContext(ctx, parsed.Results, cleanOpts)
	if err != nil {
		return ExaSearchResponse{}, Result{}, err
	}
	res := CheckAndScore(question, "exa", reports, scoreOpts)
	return parsed, res, nil
}

// ProcessExaContents runs the full Exa intake pipeline (ParseExaContents -> Clean -> Split -> Graph -> Checks -> Score)
// on a raw POST /contents response body.
func ProcessExaContents(ctx context.Context, question string, body []byte, parseOpts ParseOptions, cleanOpts CleanOptions, scoreOpts ScoreOptions) (ExaContentsResponse, Result, error) {
	parsed, err := ParseExaContents(ctx, body, parseOpts)
	if err != nil {
		return ExaContentsResponse{}, Result{}, err
	}
	reports, err := prepareExaReportsWithContext(ctx, parsed.Results, cleanOpts)
	if err != nil {
		return ExaContentsResponse{}, Result{}, err
	}
	res := CheckAndScore(question, "exa", reports, scoreOpts)
	return parsed, res, nil
}

func prepareExaReportsWithContext(ctx context.Context, results []ExaResult, cleanOpts CleanOptions) ([]Report, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return PrepareExaReports(results, cleanOpts)
}
