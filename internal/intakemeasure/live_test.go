package intakemeasure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semguard/intake"
)

func loadSavedSecondReport(path string) (SecondMeasurementReport, error) {
	b, err := os.ReadFile(path) //nolint:gosec // test helper reads repo evidence JSON
	if err != nil {
		return SecondMeasurementReport{}, err
	}
	var rep SecondMeasurementReport
	if err := json.Unmarshal(b, &rep); err != nil {
		return SecondMeasurementReport{}, err
	}
	return rep, nil
}

func waitReady(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // local loopback health check
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", url)
}

func startOwnedMetalSemselect(t *testing.T, semselectDir string) (string, func()) {
	t.Helper()
	llamaBin := filepath.Join(semselectDir, ".native", "build-6c59c40076c0", "bin", "llama-server")
	modelPath := filepath.Join(semselectDir, "models", "Kev-4B-Q4_K_M.gguf")
	guardBin := filepath.Join(semselectDir, "bin", "semselect")

	llamaCmd := exec.Command( //nolint:gosec // local pinned binary in sibling semselect repo
		llamaBin,
		"-m", modelPath,
		"--alias", "semselect-kev-4b",
		"--host", "127.0.0.1",
		"--port", "18085",
		"-ngl", "99",
		"-t", "4",
		"-tb", "4",
		"-c", "4096",
		"-b", "512",
		"-ub", "512",
		"-np", "1",
		"--no-context-shift",
		"--metrics",
		"-lv", "4",
	)
	if err := llamaCmd.Start(); err != nil {
		t.Fatalf("start llama-server: %v", err)
	}

	guardCmd := exec.Command(guardBin) //nolint:gosec // local pinned guard binary
	guardCmd.Env = append(os.Environ(),
		"SEMSELECT_ADDR=127.0.0.1:18084",
		"SEMSELECT_UPSTREAM=http://127.0.0.1:18085",
		"SEMSELECT_MODEL=semselect-kev-4b",
		"SEMSELECT_TIMEOUT=120s",
	)
	if err := guardCmd.Start(); err != nil {
		_ = llamaCmd.Process.Kill()
		_, _ = llamaCmd.Process.Wait()
		t.Fatalf("start semselect guard: %v", err)
	}

	cleanup := func() {
		if guardCmd.Process != nil {
			_ = guardCmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() {
				_ = guardCmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = guardCmd.Process.Kill()
				<-done
			}
		}
		if llamaCmd.Process != nil {
			_ = llamaCmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() {
				_ = llamaCmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = llamaCmd.Process.Kill()
				<-done
			}
		}
	}

	waitReady(t, "http://127.0.0.1:18085/health", 60*time.Second)
	waitReady(t, "http://127.0.0.1:18084/ready", 15*time.Second)
	return "http://127.0.0.1:18084", cleanup
}

//aip:verifies intake-second-measurement-stop-rule
func TestGenerateSecondMeasurementLive(t *testing.T) {
	semselectDir := os.Getenv("SEMGUARD_LIVE_SEMSELECT_DIR")
	baseURL := os.Getenv("SEMGUARD_LIVE_SEMSELECT_URL")
	runMercury := os.Getenv("SEMGUARD_LIVE_MERCURY_DECIDE") == "1"
	if semselectDir == "" && baseURL == "" && !runMercury {
		t.Skip("set SEMGUARD_LIVE_SEMSELECT_DIR, SEMGUARD_LIVE_SEMSELECT_URL, or SEMGUARD_LIVE_MERCURY_DECIDE=1 to run live second measurement")
	}

	ctx := context.Background()
	corpusDir := findCorpusDir(t)
	repoRoot := filepath.Dir(corpusDir)

	firstRep, err := RunFirstMeasurement(ctx, corpusDir)
	if err != nil {
		t.Fatalf("RunFirstMeasurement: %v", err)
	}

	secondJSONPath := filepath.Join(repoRoot, "docs", "evidence", "intake-second-measurement.json")
	mercuryJSONPath := filepath.Join(repoRoot, "docs", "evidence", "intake-second-measurement-mercury-decide.json")

	if semselectDir != "" || baseURL != "" {
		if semselectDir != "" {
			var cleanup func()
			baseURL, cleanup = startOwnedMetalSemselect(t, semselectDir)
			defer cleanup()
		}

		decider := &intake.SemselectDecider{
			BaseURL: baseURL,
			Model:   "semselect-kev-4b",
			HTTPClient: &http.Client{
				Timeout: 120 * time.Second,
			},
		}
		hw := "darwin/arm64 (Apple M3 Pro, Metal)"
		lock := DefaultKev4BLock(hw, "http://127.0.0.1:8084")

		// Warmup 1 call before timing the 181-passage cohort.
		_, _, warmupErr := decider.DecidePassage(ctx, "Warmup question", intake.ScoredPassage{
			Passage:  intake.Passage{ID: "warmup:p1", Body: "Warmup passage text for Metal runtime initialization."},
			ReportID: "warmup",
		})
		if warmupErr != nil {
			t.Fatalf("warmup DecidePassage: %v", warmupErr)
		}

		secondRep, err := RunSecondMeasurement(ctx, corpusDir, decider, lock, func(done, total int, item PassageDecisionItem) {
			if done == 1 || done%20 == 0 || done == total {
				fmt.Fprintf(os.Stderr, "[second-measurement] %d/%d (%s %s p%d, %dms, outcome=%s, max=%.4f)\n",
					done, total, item.CaseID, item.DocID, item.ParagraphNum, item.Decision.DurationMs, item.Decision.Outcome, item.Decision.MaxProb)
			}
		})
		if err != nil {
			t.Fatalf("RunSecondMeasurement: %v", err)
		}

		if err := WriteSecondEvidenceJSON(secondJSONPath, secondRep); err != nil {
			t.Fatalf("WriteSecondEvidenceJSON: %v", err)
		}
	}

	if runMercury {
		apiKey := os.Getenv("INCEPTION_API_KEY")
		if apiKey == "" {
			t.Fatal("SEMGUARD_LIVE_MERCURY_DECIDE=1 requires INCEPTION_API_KEY")
		}
		mercURL := os.Getenv("INCEPTION_BASE_URL")
		if mercURL == "" {
			mercURL = "https://api.inceptionlabs.ai"
		}
		decider := &intake.SemselectDecider{
			BaseURL: mercURL,
			Model:   "mercury-decide",
			APIKey:  apiKey,
			HTTPClient: &http.Client{
				Timeout: 60 * time.Second,
			},
		}
		lock := DefaultMercuryDecideLock("darwin/arm64 (client -> Inception Labs API)", "api.inceptionlabs.ai")

		_, _, warmupErr := decider.DecidePassage(ctx, "Warmup question", intake.ScoredPassage{
			Passage:  intake.Passage{ID: "warmup:p1", Body: "Warmup passage text for API initialization."},
			ReportID: "warmup",
		})
		if warmupErr != nil {
			t.Fatalf("mercury warmup DecidePassage: %v", warmupErr)
		}

		mercRep, err := RunSecondMeasurement(ctx, corpusDir, decider, lock, func(done, total int, item PassageDecisionItem) {
			if done == 1 || done%20 == 0 || done == total {
				fmt.Fprintf(os.Stderr, "[mercury-decide] %d/%d (%s %s p%d, %dms, outcome=%s, max=%.4f)\n",
					done, total, item.CaseID, item.DocID, item.ParagraphNum, item.Decision.DurationMs, item.Decision.Outcome, item.Decision.MaxProb)
			}
		})
		if err != nil {
			t.Fatalf("RunSecondMeasurement (mercury-decide): %v", err)
		}
		if err := WriteSecondEvidenceJSON(mercuryJSONPath, mercRep); err != nil {
			t.Fatalf("WriteSecondEvidenceJSON (mercury-decide): %v", err)
		}
	}

	savedSecond, err := loadSavedSecondReport(secondJSONPath)
	if err != nil {
		t.Fatalf("loadSavedSecondReport(%s): %v", secondJSONPath, err)
	}
	var comps []SecondMeasurementReport
	if savedMerc, merr := loadSavedSecondReport(mercuryJSONPath); merr == nil {
		comps = append(comps, savedMerc)
	}

	resultsMDPath := filepath.Join(repoRoot, "docs", "intake-results.md")
	md := RenderCombinedMarkdown(firstRep, &savedSecond, comps...)
	tmp := resultsMDPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(md), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, resultsMDPath); err != nil {
		t.Fatalf("Rename %s: %v", resultsMDPath, err)
	}
	t.Logf("wrote %s (planted=%d/%d, cleanTwin=%d/%d, decision=%s, comparisons=%d)",
		resultsMDPath,
		savedSecond.PlantedAll.Flagged, savedSecond.PlantedAll.Total,
		savedSecond.CleanMatchedTwin.Flagged, savedSecond.CleanMatchedTwin.Total,
		savedSecond.StopRule.Decision, len(comps))
}
