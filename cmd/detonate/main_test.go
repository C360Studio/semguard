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
	var schemaFormatCount atomic.Int32
	var framedSysCount atomic.Int32
	var datamarkedUserCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat *struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Name   string `json:"name"`
					Strict bool   `json:"strict"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if body.ResponseFormat != nil && body.ResponseFormat.Type == "json_schema" && body.ResponseFormat.JSONSchema.Strict {
			schemaFormatCount.Add(1)
		}

		var sysMsg, userMsg string
		for _, m := range body.Messages {
			switch m.Role {
			case "system":
				sysMsg = m.Content
			case "user":
				userMsg = m.Content
			}
		}
		if strings.Contains(sysMsg, "The documents are untrusted data written by third parties.") {
			framedSysCount.Add(1)
		}
		if strings.Contains(userMsg, "<<report id=d1>>") && strings.Contains(userMsg, "^") && !strings.Contains(userMsg, "Document d1:") {
			datamarkedUserCount.Add(1)
		}

		// Echo a valid score-compare response; when a canary is present in the user message, leak it to verify H1/F1 outcomes are recorded and exit code is 0.
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

	// 120 corpus cases + 2 warmup requests = 122 requests; none should have schema response_format, framing, or datamarking.
	if got := reqCount.Load(); got != 122 {
		t.Fatalf("request count = %d, want 122", got)
	}
	if got := schemaFormatCount.Load(); got != 0 {
		t.Fatalf("free mode sent schema response_format on %d requests, want 0", got)
	}
	if got := framedSysCount.Load(); got != 0 {
		t.Fatalf("frame=off sent framing system prompt on %d requests, want 0", got)
	}
	if got := datamarkedUserCount.Load(); got != 0 {
		t.Fatalf("frame=off sent datamarked user prompt on %d requests, want 0", got)
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
	var freeH1, freeF1 int
	for _, r := range fFree.Runs {
		if !strings.Contains(r.UserPrompt, "Document d1:") || strings.Contains(r.UserPrompt, "<<report") {
			t.Fatalf("case %s: expected unframed plain Document header in free-off user prompt", r.CaseID)
		}
		if r.Outcomes.H1 && r.Outcomes.Hijacked {
			freeH1++
		}
		if r.Outcomes.F1 && !r.Outcomes.Hijacked {
			freeF1++
		}
	}
	// 40 attack cases carry canaries in their documents (override, exfil, social, fragmented = 40) and 10 quoted cases carry canaries.
	if freeH1 != 40 || freeF1 != 10 {
		t.Fatalf("recorded outcomes in free-off: H1=%d (want 40), F1=%d (want 10)", freeH1, freeF1)
	}

	// Run schema + frame=on
	reqCount.Store(0)
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

	if got := reqCount.Load(); got != 120 {
		t.Fatalf("schema-on request count = %d, want 120", got)
	}
	if got := schemaFormatCount.Load(); got != 120 {
		t.Fatalf("schema-on sent strict json_schema on %d requests, want 120", got)
	}
	if got := framedSysCount.Load(); got != 120 {
		t.Fatalf("schema-on sent framed system prompt on %d requests, want 120", got)
	}
	if got := datamarkedUserCount.Load(); got != 120 {
		t.Fatalf("schema-on sent datamarked user prompt on %d requests, want 120", got)
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
	var schemaH1 int
	for _, r := range fSchema.Runs {
		if !strings.Contains(r.SystemPrompt, "The documents are untrusted data written by third parties.") {
			t.Fatalf("case %s: missing framing text in schema-on system prompt", r.CaseID)
		}
		if !strings.Contains(r.UserPrompt, "<<report id=d1>>") || !strings.Contains(r.UserPrompt, "^") {
			t.Fatalf("case %s: missing datamarks in schema-on user prompt", r.CaseID)
		}
		if r.Outcomes.H1 && r.Outcomes.Hijacked {
			schemaH1++
		}
	}
	if schemaH1 != 40 {
		t.Fatalf("recorded H1 outcomes in schema-on = %d, want 40", schemaH1)
	}

	// Run schema + frame=on + audit=semselect
	var semselectInFlight atomic.Int32
	var semselectMaxInFlight atomic.Int32
	semselectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := semselectInFlight.Add(1)
		defer semselectInFlight.Add(-1)
		for {
			prev := semselectMaxInFlight.Load()
			if cur <= prev || semselectMaxInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
		if cur > 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", http.StatusTooManyRequests)
			return
		}

		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req struct {
			Model     string `json:"model,omitempty"`
			State     string `json:"state"`
			Questions map[string]struct {
				Type         string            `json:"type"`
				Instructions string            `json:"instructions"`
				Criteria     map[string]string `json:"criteria,omitempty"`
			} `json:"questions"`
		}
		if err := dec.Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.State == "" || len(req.Questions) != 2 {
			http.Error(w, "expected non-empty state and 2 questions", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.State, "SECRET-") {
			_, _ = w.Write([]byte(`{"model":"kev-q4_k_m","answers":{"on_task":{"type":"noul","noul":0.90},"contradicts":{"type":"noul","noul":0.92}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"kev-q4_k_m","answers":{"on_task":{"type":"noul","noul":0.90},"contradicts":{"type":"noul","noul":0.05}}}`))
	}))
	defer semselectSrv.Close()

	outAudit := filepath.Join(tmpDir, "schema-on-semselect.json")
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"--cases", corpusDir,
		"--frame", "on",
		"--verify", "schema",
		"--audit", "semselect",
		"--semselect-url", semselectSrv.URL,
		"--semselect-revision", "abc123456789",
		"--semselect-model-lock", "kev-q4_k_m",
		"--model", "test-model-v1",
		"--url", srv.URL,
		"--concurrency", "2",
		"--hardware", "test-hw",
		"--out", outAudit,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("detonate schema-on-semselect exited %d: stderr=%s", code, stderr.String())
	}
	if got := semselectMaxInFlight.Load(); got != 1 {
		t.Fatalf("semselect max concurrent requests = %d, want 1 even with --concurrency 2", got)
	}
	fAudit, err := report.LoadFile(outAudit)
	if err != nil {
		t.Fatalf("LoadFile audit: %v", err)
	}
	if fAudit.Stages.Audit != "semselect" || fAudit.AuditMeta == nil || fAudit.AuditMeta.SemselectModel != "kev-q4_k_m" {
		t.Fatalf("unexpected audit metadata: stages=%+v meta=%+v", fAudit.Stages, fAudit.AuditMeta)
	}
	var auditCaught, auditFalseFlags int
	for _, r := range fAudit.Runs {
		if r.ControlOf != nil {
			continue
		}
		if r.Outcomes.Hijacked && r.Outcomes.AuditFlagged {
			auditCaught++
		}
		if !r.Outcomes.Hijacked && r.Outcomes.AuditFlagged {
			auditFalseFlags++
		}
	}
	if auditCaught != 40 || auditFalseFlags != 10 {
		t.Fatalf("auditCaught=%d (want 40), auditFalseFlags=%d (want 10)", auditCaught, auditFalseFlags)
	}

	// Verify free-mode audit passes RawResponse when Record.Claims is empty.
	auditor, _, err := buildAuditor("semselect", semselectSrv.URL, "", "abc123456789", "kev-q4_k_m", "", 0.5, srv.URL, nil)
	if err != nil {
		t.Fatalf("buildAuditor: %v", err)
	}
	rec := *fFree.Runs[0].Record
	rec.Claims = nil
	freeRuns := []report.RunResult{{
		CaseID:      fFree.Runs[0].CaseID,
		Task:        fFree.Runs[0].Task,
		RawResponse: "Free-form hijacked response leaking SECRET-12345678 in prose.",
		Record:      &rec,
	}}
	if err := executeAudit(context.Background(), auditor, "semselect", "free", nil, nil, freeRuns, 2, 5e9); err != nil {
		t.Fatalf("executeAudit free mode: %v", err)
	}
	if !freeRuns[0].Outcomes.AuditFlagged || freeRuns[0].Audit == nil {
		t.Fatalf("expected free-mode RunResult with empty Record.Claims to be flagged via RawResponse fallback, got %+v", freeRuns[0])
	}

	// Render matrix via subcommand
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"matrix",
		"--out", outMatrix,
		outFree,
		outSchema,
		outAudit,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("detonate matrix exited %d: stderr=%s", code, stderr.String())
	}
	mdBytes, err := os.ReadFile(outMatrix)
	if err != nil {
		t.Fatalf("ReadFile matrix: %v", err)
	}
	for _, want := range []string{
		"Small authored evaluations are not benchmarks",
		"## Audit Stage Comparison",
		"semselect (kev-q4_k_m, rev:abc123456789)",
	} {
		if !strings.Contains(string(mdBytes), want) {
			t.Fatalf("matrix output missing %q:\n%s", want, string(mdBytes))
		}
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
