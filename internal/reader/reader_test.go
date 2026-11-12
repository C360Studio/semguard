package reader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkStrictObjectSchema(t *testing.T, path string, node map[string]any) {
	t.Helper()
	if node["type"] == "object" {
		if ap, ok := node["additionalProperties"].(bool); !ok || ap {
			t.Fatalf("%s: strict object must set additionalProperties: false, got %v", path, node["additionalProperties"])
		}
		props, _ := node["properties"].(map[string]any)
		reqRaw, _ := node["required"].([]any)
		reqSet := make(map[string]bool, len(reqRaw))
		for _, r := range reqRaw {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
		for k, v := range props {
			if !reqSet[k] {
				t.Fatalf("%s: property %q is not in required %v (invalid for OpenAI strict Structured Outputs)", path, k, reqRaw)
			}
			if sub, ok := v.(map[string]any); ok {
				checkStrictObjectSchema(t, path+"."+k, sub)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		checkStrictObjectSchema(t, path+"[]", items)
	}
}

//aip:verifies reader-complete-free
func TestCompleteFreeMode(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"d1: 8, d2: 4"}}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1", APIKey: "test-key"}
	resp, err := c.Complete(context.Background(), Request{
		Model:  "test-model",
		System: "system instruction",
		User:   "user question",
		Mode:   ModeFree,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp.Content != "d1: 8, d2: 4" || resp.Refused || resp.SchemaSHA256 != "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q, want Bearer test-key", gotAuth)
	}
	if _, hasFormat := gotBody["response_format"]; hasFormat {
		t.Fatalf("free mode must not send response_format: %+v", gotBody)
	}
	if _, hasTools := gotBody["tools"]; hasTools {
		t.Fatalf("reader must never send tools: %+v", gotBody)
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %+v, want 2 messages", gotBody["messages"])
	}
}

//aip:verifies reader-complete-schema
func TestCompleteSchemaModeAndLoadSchema(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "eval", "schemas", "score-compare.json")
	schemaBytes, wantSHA, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"scores\":[],\"claims\":[],\"confidence\":0.9}"}}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	resp, err := c.Complete(context.Background(), Request{
		Model:      "test-model",
		System:     "sys",
		User:       "usr",
		Mode:       ModeSchema,
		TaskID:     "score-compare",
		SchemaJSON: schemaBytes,
	})
	if err != nil {
		t.Fatalf("Complete schema mode failed: %v", err)
	}
	if resp.SchemaSHA256 != wantSHA {
		t.Fatalf("SchemaSHA256 = %s, want %s", resp.SchemaSHA256, wantSHA)
	}
	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %+v, want type=json_schema", gotBody["response_format"])
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok || js["name"] != "score-compare" || js["strict"] != true {
		t.Fatalf("json_schema = %+v", rf["json_schema"])
	}
	transmittedSchema, ok := js["schema"].(map[string]any)
	if !ok {
		t.Fatalf("transmitted schema is not an object: %+v", js["schema"])
	}
	checkStrictObjectSchema(t, "score-compare", transmittedSchema)
}

//aip:verifies reader-refusal
func TestCompleteRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":null,"refusal":"I cannot fulfill this request."}}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	resp, err := c.Complete(context.Background(), Request{
		Model: "test-model",
		User:  "usr",
		Mode:  ModeFree,
	})
	if err != nil {
		t.Fatalf("refusal should not be a transport error, got %v", err)
	}
	if !resp.Refused || resp.Refusal != "I cannot fulfill this request." {
		t.Fatalf("want Refused=true with message, got %+v", resp)
	}
}

//aip:verifies reader-bounded-response
func TestCompleteBoundedResponseAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oversized/chat/completions":
			_, _ = w.Write([]byte(strings.Repeat("x", 256)))
		case "/status500/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream error"))
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/oversized", MaxResponseBytes: 64}
	if _, err := c.Complete(context.Background(), Request{Model: "m", User: "u"}); err == nil || !strings.Contains(err.Error(), "exceeds max 64 bytes") {
		t.Fatalf("want oversized response error, got %v", err)
	}

	c500 := &Client{BaseURL: srv.URL + "/status500"}
	if _, err := c500.Complete(context.Background(), Request{Model: "m", User: "u"}); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("want HTTP 500 error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c500.Complete(ctx, Request{Model: "m", User: "u"}); err == nil {
		t.Fatal("canceled context must fail")
	}
}

// TestTransient pins which Complete errors a caller may retry: request timeout, connection error, HTTP 429, and
// HTTP 5xx. A 200 with a malformed body and any other 4xx are not transient.
//
//aip:verifies reader-transient-errors
func TestTransient(t *testing.T) {
	release := make(chan struct{}) // frees the /hang handler so srv.Close does not wait on it
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/s429/chat/completions":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/s500/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
		case "/s503/chat/completions":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/s400/chat/completions":
			w.WriteHeader(http.StatusBadRequest)
		case "/s401/chat/completions":
			w.WriteHeader(http.StatusUnauthorized)
		case "/s404/chat/completions":
			w.WriteHeader(http.StatusNotFound)
		case "/malformed/chat/completions":
			_, _ = w.Write([]byte("{not json"))
		case "/hang/chat/completions":
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
	}))
	defer srv.Close()
	defer close(release)

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()

	call := func(base string, ctx context.Context) error {
		_, err := (&Client{BaseURL: base}).Complete(ctx, Request{Model: "m", User: "u"})
		if err == nil {
			t.Fatalf("%s: want an error", base)
		}
		return err
	}
	for path, want := range map[string]bool{
		"/s429": true, "/s500": true, "/s503": true,
		"/s400": false, "/s401": false, "/s404": false, "/malformed": false,
	} {
		if got := Transient(call(srv.URL+path, context.Background())); got != want {
			t.Errorf("%s: Transient = %v, want %v", path, got, want)
		}
	}
	if !Transient(call(closedURL, context.Background())) {
		t.Error("connection refused: Transient = false, want true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if !Transient(call(srv.URL+"/hang", ctx)) {
		t.Error("request timeout: Transient = false, want true")
	}
	if Transient(nil) {
		t.Error("Transient(nil) = true")
	}
}
