package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semguard/internal/report"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "corpus", "corpus.lock.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root with corpus/corpus.lock.json")
		}
		dir = parent
	}
}

//aip:verifies detonate-stage-wiring detonate-hijack-exits-zero
func TestDetonateStageWiringAndHijackExitZero(t *testing.T) {
	root := findRepoRoot(t)
	corpusDir := filepath.Join(root, "corpus")

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat *struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Echo a valid score-compare response; for override-001 include its canary to verify H1 exits 0.
		userMsg := ""
		for _, m := range body.Messages {
			if m.Role == "user" {
				userMsg = m.Content
			}
		}
		claim := "clean summary"
		if idx := strings.Index(userMsg, "SECRET-"); idx >= 0 && idx+15 <= len(userMsg) {
			claim = "leaked " + userMsg[idx:idx+15]
		}
		content := fmt.Sprintf(`{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5},{"doc_id":"d3","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":%q,"quote":"a"}],"confidence":0.8}`, claim)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"stop","message":{"content":%q}}]}`, content)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFree := filepath.Join(tmpDir, "free-off.json")
	outSchema := filepath.Join(tmpDir, "schema-on.json")
	outMatrix := filepath.Join(tmpDir, "results.md")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"--cases", filepath.Join(corpusDir, "cases.json"),
		"--frame", "off",
		"--verify", "free",
		"--model", "test-model-v1",
		"--url", srv.URL + "/v1?secret=ignore",
		"--key", "test-key",
		"--warmup", "2",
		"--concurrency", "2",
		"--hardware", "test-hw",
		"--out", outFree,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("detonate free-off exited %d: stderr=%s", code, stderr.String())
	}

	// 120 corpus cases + 2 warmup requests = 122 requests
	if got := reqCount.Load(); got != 122 {
		t.Fatalf("request count = %d, want 122", got)
	}

	fFree, err := report.LoadFile(outFree)
	if err != nil {
		t.Fatalf("LoadFile free: %v", err)
	}
	if fFree.Model != "test-model-v1" || fFree.EndpointHost != srv.URL || fFree.Hardware != "test-hw" {
		t.Fatalf("unexpected metadata: model=%q host=%q hw=%q", fFree.Model, fFree.EndpointHost, fFree.Hardware)
	}
	if fFree.Stages.Datamark || fFree.Stages.Framing || fFree.Stages.Verify != "free" {
		t.Fatalf("unexpected stages: %+v", fFree.Stages)
	}
	if fFree.FramingSHA256 != "" || fFree.SchemaSHA256 != "" {
		t.Fatalf("expected empty framing and schema hashes in free-off, got framing=%q schema=%q", fFree.FramingSHA256, fFree.SchemaSHA256)
	}
	if len(fFree.Runs) != 120 {
		t.Fatalf("len(Runs) = %d, want 120", len(fFree.Runs))
	}

	// Run schema + frame=on
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"--cases", corpusDir,
		"--frame", "on",
		"--verify", "schema",
		"--model", "test-model-v1",
		"--url", srv.URL,
		"--concurrency", "2",
		"--hardware", "test-hw",
		"--out", outSchema,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("detonate schema-on exited %d: stderr=%s", code, stderr.String())
	}

	fSchema, err := report.LoadFile(outSchema)
	if err != nil {
		t.Fatalf("LoadFile schema: %v", err)
	}
	if !fSchema.Stages.Datamark || !fSchema.Stages.Framing || fSchema.Stages.Verify != "schema" {
		t.Fatalf("unexpected stages: %+v", fSchema.Stages)
	}
	if fSchema.FramingSHA256 == "" || fSchema.SchemaSHA256 == "" {
		t.Fatalf("expected non-empty framing and schema hashes in schema-on, got framing=%q schema=%q", fSchema.FramingSHA256, fSchema.SchemaSHA256)
	}

	// Render matrix via subcommand
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"matrix",
		"--out", outMatrix,
		outFree,
		outSchema,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("detonate matrix exited %d: stderr=%s", code, stderr.String())
	}
	mdBytes, err := os.ReadFile(outMatrix)
	if err != nil {
		t.Fatalf("ReadFile matrix: %v", err)
	}
	if !strings.Contains(string(mdBytes), "Small authored evaluations are not benchmarks") {
		t.Fatalf("matrix output missing benchmark caveat:\n%s", string(mdBytes))
	}
}

//aip:verifies detonate-transport-error-exits-nonzero
func TestDetonateTransportErrorExitsNonzero(t *testing.T) {
	root := findRepoRoot(t)
	corpusDir := filepath.Join(root, "corpus")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failure", http.StatusBadGateway)
	}))
	defer srv.Close()

	outPath := filepath.Join(t.TempDir(), "fail.json")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"--cases", corpusDir,
		"--frame", "off",
		"--verify", "free",
		"--model", "test-model",
		"--url", srv.URL,
		"--out", outPath,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected nonzero exit code on HTTP 502 transport error, got 0")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected no output file written on transport failure, stat err=%v", err)
	}
}

//aip:verifies detonate-stage-wiring
func TestParseFrameSwitches(t *testing.T) {
	tests := []struct {
		frame        string
		datamark     string
		wantDatamark bool
		wantFraming  bool
		wantErr      bool
	}{
		{"off", "", false, false, false},
		{"on", "", true, true, false},
		{"datamark", "", true, false, false},
		{"framing", "", false, true, false},
		{"on", "off", false, true, false},
		{"off", "on", true, false, false},
		{"invalid", "", false, false, true},
		{"off", "invalid", false, false, true},
	}
	for _, tt := range tests {
		opts, err := parseFrameSwitches(tt.frame, tt.datamark)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseFrameSwitches(%q, %q) err=%v, wantErr=%v", tt.frame, tt.datamark, err, tt.wantErr)
		}
		if !tt.wantErr && (opts.Datamark != tt.wantDatamark || opts.Framing != tt.wantFraming) {
			t.Fatalf("parseFrameSwitches(%q, %q) = %+v, want Datamark=%v Framing=%v", tt.frame, tt.datamark, opts, tt.wantDatamark, tt.wantFraming)
		}
	}
}
