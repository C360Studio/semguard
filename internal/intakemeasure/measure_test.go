package intakemeasure

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semguard/intake"
	"github.com/c360studio/semguard/internal/corpus"
	"github.com/c360studio/servicesim/profiles/exa"
	"github.com/c360studio/servicesim/testkit"
)

func findCorpusDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../corpus",
		"corpus",
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "corpus.lock.json")); err == nil {
			return c
		}
	}
	t.Fatal("could not locate corpus/corpus.lock.json")
	return ""
}

//aip:verifies intake-first-measurement-corpus-via-servicesim
func TestFirstMeasurementCorpusViaServicesim(t *testing.T) {
	ctx := context.Background()
	corpusDir := findCorpusDir(t)

	cases, _, corpusSHA, err := corpus.LoadWithHash(corpusDir)
	if err != nil {
		t.Fatalf("LoadWithHash(%s): %v", corpusDir, err)
	}
	if len(cases) != 140 {
		t.Fatalf("len(cases) = %d, want 140", len(cases))
	}

	// 1. Verify mechanical converter builds 80 scenario pairs (60 attack + control twin pairs + 20 benign cases).
	pairs, err := BuildCorpusScenarioPairs(ctx, cases)
	if err != nil {
		t.Fatalf("BuildCorpusScenarioPairs: %v", err)
	}
	if len(pairs) != 80 {
		t.Fatalf("len(pairs) = %d, want 80", len(pairs))
	}
	withControl := 0
	for _, p := range pairs {
		if len(p.AttackYAML) == 0 {
			t.Fatalf("empty AttackYAML for %s", p.CaseID)
		}
		if p.HasControlTwin {
			withControl++
			if len(p.ControlYAML) == 0 || p.ControlCaseID != p.CaseID+"-control" {
				t.Fatalf("invalid control twin for %s: id=%q len=%d", p.CaseID, p.ControlCaseID, len(p.ControlYAML))
			}
		}
	}
	if withControl != 60 {
		t.Fatalf("pairs with control twin = %d, want 60", withControl)
	}

	// 2. Verify testkit.Start serves a converted attack scenario and its clean twin scenario end-to-end.
	firstPair := pairs[0]
	for _, yamlDoc := range [][]byte{firstPair.AttackYAML, firstPair.ControlYAML} {
		sim := testkit.Start(
			t,
			testkit.WithScenarioYAML(string(yamlDoc)),
			testkit.WithProfiles(exa.Profile()),
		)
		reqBody, _ := json.Marshal(map[string]any{"query": "test query", "numResults": firstPair.DocumentCount})
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, sim.URL(exa.Name)+"/search", bytes.NewReader(reqBody))
		if reqErr != nil {
			t.Fatalf("NewRequest: %v", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "test-exa-key")
		resp, doErr := sim.Client().Do(req)
		if doErr != nil {
			t.Fatalf("sim.Client().Do: %v", doErr)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d err=%v", resp.StatusCode, readErr)
		}
		parsed, res, procErr := intake.ProcessExaSearch(ctx, "test query", body, intake.ParseOptions{}, intake.CleanOptions{}, intake.ScoreOptions{})
		if procErr != nil {
			t.Fatalf("ProcessExaSearch: %v", procErr)
		}
		if len(parsed.Results) != firstPair.DocumentCount || len(res.Reports) != firstPair.DocumentCount {
			t.Fatalf("unexpected results/reports count: %d / %d, want %d", len(parsed.Results), len(res.Reports), firstPair.DocumentCount)
		}
	}

	// 3. Run the complete first measurement across all 140 corpus cases via servicesim.
	rep, err := EvaluateCasesViaServicesim(ctx, cases, corpusSHA)
	if err != nil {
		t.Fatalf("EvaluateCasesViaServicesim: %v", err)
	}
	if rep.TotalCases != 140 || rep.AttackCases != 60 || rep.ControlTwinCases != 60 || rep.BenignCases != 20 {
		t.Fatalf("unexpected case counts: total=%d attack=%d ctrl=%d benign=%d",
			rep.TotalCases, rep.AttackCases, rep.ControlTwinCases, rep.BenignCases)
	}

	// 4. Verify paragraph-level oracle counts: 80 planted paragraphs (5 classes x 10 + fragmented 10 x 3),
	// 539 clean paragraphs in the 60 -control twins, and 1,269 total clean paragraphs.
	p := rep.ParagraphPassages
	if p.PlantedTotal != 80 {
		t.Fatalf("ParagraphPassages.PlantedTotal = %d, want 80", p.PlantedTotal)
	}
	if p.CleanTwinTotal != 539 {
		t.Fatalf("ParagraphPassages.CleanTwinTotal = %d, want 539", p.CleanTwinTotal)
	}
	if p.CleanAllTotal != 1269 {
		t.Fatalf("ParagraphPassages.CleanAllTotal = %d, want 1269", p.CleanAllTotal)
	}
	if p.PlantedInitial.Low+p.PlantedInitial.Middle+p.PlantedInitial.High != p.PlantedTotal {
		t.Fatalf("planted band sum mismatch: %+v", p.PlantedInitial)
	}
	if p.CleanTwinInitial.Low+p.CleanTwinInitial.Middle+p.CleanTwinInitial.High != p.CleanTwinTotal {
		t.Fatalf("clean twin band sum mismatch: %+v", p.CleanTwinInitial)
	}

	// 5. Verify separate rank class reporting and overt class recall.
	if p.RankClass.Class != "rank" || p.RankClass.PlantedTotal != 10 || p.RankClass.CleanTwinTotal != 90 {
		t.Fatalf("unexpected RankClass summary: %+v", p.RankClass)
	}
	if p.RankClass.PlantedByCheck[intake.CheckSelfRankClaim] != 5 || p.RankClass.PlantedInitial.MiddleOrHigh != 5 {
		t.Fatalf("expected 5/10 rank planted passages caught by self_rank_claim and in middle+high, got %+v", p.RankClass)
	}
	if p.PlantedByCheck[intake.CheckUnlistedLink] != 10 || p.CleanTwinByCheck[intake.CheckUnlistedLink] != 0 {
		t.Fatalf("expected 10/80 planted and 0/539 clean twin hits for unlisted_link, got %d and %d",
			p.PlantedByCheck[intake.CheckUnlistedLink], p.CleanTwinByCheck[intake.CheckUnlistedLink])
	}

	// 6. Verify stop-rule evaluation (initial 35/80 = 43.8% < 80%; tuned 49/80 = 61.3% < 80% and clean 23.4% > 10%).
	if p.StopRuleInitial.CarryOnWithBands {
		t.Fatalf("expected StopRuleInitial.CarryOnWithBands = false, got true (%+v)", p.StopRuleInitial)
	}
	if p.StopRuleTuned.CarryOnWithBands {
		t.Fatalf("expected StopRuleTuned.CarryOnWithBands = false, got true (%+v)", p.StopRuleTuned)
	}
	if p.StopRuleInitial.Decision != "drop_bands_keep_checks_as_context" {
		t.Fatalf("unexpected StopRuleInitial.Decision = %q", p.StopRuleInitial.Decision)
	}

	// 7. Verify rendered Markdown includes required non-benchmark caption, separate rank section, and tuned disclosure.
	md := RenderMarkdown(rep)
	for _, wantSub := range []string{
		"Small authored evaluations are not benchmarks.",
		"Separate `rank` Class Breakdown",
		"tuned once on this result",
		"drop_bands_keep_checks_as_context",
	} {
		if !strings.Contains(md, wantSub) {
			t.Errorf("RenderMarkdown missing %q", wantSub)
		}
	}

	// 8. Verify EvaluateStopRule boundary conditions directly.
	passStop := EvaluateStopRule(
		BandTally{Total: 10, Low: 2, Middle: 5, High: 3, MiddleOrHigh: 8, MiddleHighRate: 0.80},
		BandTally{Total: 100, Low: 90, Middle: 8, High: 2, MiddleOrHigh: 10, MiddleHighRate: 0.10},
		BandTally{Total: 100, Low: 90, Middle: 8, High: 2, MiddleOrHigh: 10, MiddleHighRate: 0.10},
	)
	if !passStop.CarryOnWithBands || passStop.Decision != "carry_on_with_bands" {
		t.Fatalf("expected boundary 80%%/10%% to pass stop rule, got %+v", passStop)
	}
	failCleanStop := EvaluateStopRule(
		BandTally{Total: 10, Low: 1, Middle: 6, High: 3, MiddleOrHigh: 9, MiddleHighRate: 0.90},
		BandTally{Total: 100, Low: 89, Middle: 9, High: 2, MiddleOrHigh: 11, MiddleHighRate: 0.11},
		BandTally{Total: 100, Low: 89, Middle: 9, High: 2, MiddleOrHigh: 11, MiddleHighRate: 0.11},
	)
	if failCleanStop.CarryOnWithBands {
		t.Fatalf("expected 11%% clean rate to fail stop rule, got %+v", failCleanStop)
	}
}
