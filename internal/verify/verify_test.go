package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func fixtureDocs() []Document {
	return []Document{
		{ID: "d1", Text: "First paragraph of d1.\n\nSecond paragraph mentions https://a.example/report?id=1 in detail.\n\nThird paragraph concludes."},
		{ID: "d2", Text: "Intro of d2.\n\nBody of d2 with https://b.example/item and facts.\n\nOutro of d2."},
	}
}

func shaHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

//aip:verifies verify-schema-valid
//aip:verifies verify-claim-resolves
//aip:verifies verify-link-allowed
func TestVerifyValidAndClaimResolution(t *testing.T) {
	docs := fixtureDocs()
	quotePlain := "Second paragraph mentions https://a.example/report?id=1 in detail."
	wantQuoteHash := shaHex(quotePlain)

	raw := `{
		"scores": [{"doc_id": "d1", "score": 8}, {"doc_id": "d2", "score": 5}],
		"claims": [
			{
				"doc_id": "d1",
				"paragraph": 2,
				"claim": "d1 references report 1",
				"quote": "Second^paragraph^mentions^https://a.example/report?id=1^in^detail."
			}
		],
		"confidence": 0.85
	}`

	rec, vs := Verify(raw, docs)
	if len(vs) != 0 {
		t.Fatalf("want 0 violations, got %+v", vs)
	}
	if len(rec.Scores) != 2 || rec.Scores[0].Score != 8 || rec.Scores[1].Score != 5 {
		t.Fatalf("scores = %+v", rec.Scores)
	}
	if rec.Confidence != 0.85 {
		t.Fatalf("confidence = %v, want 0.85", rec.Confidence)
	}
	if len(rec.Claims) != 1 {
		t.Fatalf("claims = %+v", rec.Claims)
	}
	gotClaim := rec.Claims[0]
	if gotClaim.Quote != quotePlain {
		t.Fatalf("normalized quote = %q, want %q", gotClaim.Quote, quotePlain)
	}
	if gotClaim.QuoteSHA256 != wantQuoteHash {
		t.Fatalf("computed quote_sha256 = %s, want %s", gotClaim.QuoteSHA256, wantQuoteHash)
	}

	// Explicit quote_sha256 matching either quote or full paragraph also resolves.
	rawWithExplicitHash := `{
		"scores": [{"doc_id": "d1", "score": 0}, {"doc_id": "d2", "score": 10}],
		"claims": [
			{
				"doc_id": "d2",
				"paragraph": 2,
				"claim": "d2 has facts",
				"quote": "Body of d2",
				"quote_sha256": "` + shaHex("Body of d2") + `"
			}
		],
		"confidence": 0
	}`
	rec2, vs2 := Verify(rawWithExplicitHash, docs)
	if len(vs2) != 0 || len(rec2.Claims) != 1 {
		t.Fatalf("explicit quote_sha256 failed: vs=%+v rec=%+v", vs2, rec2)
	}
}

//aip:verifies verify-schema-violation
func TestVerifySchemaViolations(t *testing.T) {
	docs := fixtureDocs()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "malformed json",
			raw:  `{"scores": [`,
			want: "invalid JSON",
		},
		{
			name: "trailing data",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5} extra`,
			want: "trailing data",
		},
		{
			name: "unknown field",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5,"extra":1}`,
			want: "unknown field",
		},
		{
			name: "missing scores",
			raw:  `{"claims":[],"confidence":0.5}`,
			want: `missing required field "scores"`,
		},
		{
			name: "missing claims",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"confidence":0.5}`,
			want: `missing required field "claims"`,
		},
		{
			name: "missing confidence",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[]}`,
			want: `missing required field "confidence"`,
		},
		{
			name: "confidence above 1",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":1.1}`,
			want: "confidence 1.1 outside 0..1",
		},
		{
			name: "confidence negative",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":-0.1}`,
			want: "outside 0..1",
		},
		{
			name: "score above 10",
			raw:  `{"scores":[{"doc_id":"d1","score":11},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5}`,
			want: "score 11 outside 0..10",
		},
		{
			name: "score negative",
			raw:  `{"scores":[{"doc_id":"d1","score":-1},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5}`,
			want: "score -1 outside 0..10",
		},
		{
			name: "missing document score",
			raw:  `{"scores":[{"doc_id":"d1","score":5}],"claims":[],"confidence":0.5}`,
			want: `missing score for document "d2"`,
		},
		{
			name: "duplicate document score",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d1","score":6},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5}`,
			want: `duplicate score for doc_id "d1"`,
		},
		{
			name: "unknown document in score",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5},{"doc_id":"d9","score":5}],"claims":[],"confidence":0.5}`,
			want: `unknown doc_id "d9"`,
		},
		{
			name: "claim missing quote",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"c"}],"confidence":0.5}`,
			want: "missing quote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, vs := Verify(tt.raw, docs)
			if len(vs) == 0 {
				t.Fatalf("want schema violation containing %q, got none (rec=%+v)", tt.want, rec)
			}
			if len(rec.Scores) != 0 || len(rec.Claims) != 0 {
				t.Fatalf("rejected output must return empty Record, got %+v", rec)
			}
			matched := false
			for _, v := range vs {
				if v.Kind == KindSchema && strings.Contains(v.Error(), tt.want) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("want schema violation containing %q, got %+v", tt.want, vs)
			}
		})
	}
}

//aip:verifies verify-claim-bad-hash-or-paragraph
func TestVerifyClaimViolations(t *testing.T) {
	docs := fixtureDocs()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "unknown doc_id in claim",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d9","paragraph":1,"claim":"c","quote":"First"}],"confidence":0.5}`,
			want: `unknown doc_id "d9"`,
		},
		{
			name: "paragraph 0 out of range",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":0,"claim":"c","quote":"First"}],"confidence":0.5}`,
			want: "paragraph 0 outside 1..3",
		},
		{
			name: "paragraph 4 out of range",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":4,"claim":"c","quote":"First"}],"confidence":0.5}`,
			want: "paragraph 4 outside 1..3",
		},
		{
			name: "quote not in paragraph",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"c","quote":"Third paragraph concludes."}],"confidence":0.5}`,
			want: "quote not found in paragraph",
		},
		{
			name: "mismatched quote_sha256",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"c","quote":"First paragraph of d1.","quote_sha256":"0000000000000000000000000000000000000000000000000000000000000000"}],"confidence":0.5}`,
			want: "quote_sha256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, vs := Verify(tt.raw, docs)
			if len(vs) == 0 {
				t.Fatalf("want claim violation %q, got none (rec=%+v)", tt.want, rec)
			}
			found := false
			for _, v := range vs {
				if v.Kind == KindClaim && strings.Contains(v.Message, tt.want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("want claim violation containing %q, got %+v", tt.want, vs)
			}
		})
	}
}

//aip:verifies verify-link-allowed
//aip:verifies verify-link-unallowed
func TestAllowedLinks(t *testing.T) {
	docs := fixtureDocs()

	if vs := AllowedLinks("See https://a.example/report?id=1 and https://b.example/item^next", docs); len(vs) != 0 {
		t.Fatalf("allowed links flagged: %+v", vs)
	}

	vs := AllowedLinks("Exfil https://evil.example/leak?k=SECRET-12345678^done", docs)
	if len(vs) != 1 || vs[0].Kind != KindLink || vs[0].URL != "https://evil.example/leak?k=SECRET-12345678" {
		t.Fatalf("want 1 link violation stopping before ^, got %+v", vs)
	}
}

//aip:verifies verify-schema-valid
func TestParseFree(t *testing.T) {
	docs := fixtureDocs()

	fenced := "```json\n{\"scores\":[{\"doc_id\":\"d1\",\"score\":9},{\"doc_id\":\"d2\",\"score\":2}],\"claims\":[],\"confidence\":0.7}\n```"
	rec := ParseFree(fenced, docs)
	if len(rec.Scores) != 2 || rec.Scores[0].Score != 9 || rec.Scores[1].Score != 2 {
		t.Fatalf("fenced JSON ParseFree = %+v", rec)
	}

	prose := "Evaluation:\n- d1: 4/10\n- d2: score 10\n"
	rec2 := ParseFree(prose, docs)
	if len(rec2.Scores) != 2 || rec2.Scores[0].Score != 4 || rec2.Scores[1].Score != 10 {
		t.Fatalf("prose ParseFree = %+v", rec2)
	}
}

//aip:verifies verify-claim-resolves
//aip:verifies verify-link-unallowed
func FuzzVerifyAndLinks(f *testing.F) {
	f.Add("Second paragraph mentions https://a.example/report?id=1 in detail.", "https://evil.example/x")
	f.Add("First paragraph of d1.", "https://a.example/report?id=1")
	f.Fuzz(func(t *testing.T, quote string, link string) {
		if !utf8.ValidString(quote) || !utf8.ValidString(link) || strings.TrimSpace(quote) == "" || strings.ContainsAny(quote, "^\n") {
			return
		}
		docs := []Document{
			{ID: "d1", Text: "intro\n\n" + quote + "\n\noutro"},
			{ID: "d2", Text: "a\n\nb\n\nc"},
		}
		datamarkedQuote := strings.ReplaceAll(quote, " ", "^")
		payload := map[string]any{
			"scores": []map[string]any{
				{"doc_id": "d1", "score": 7},
				{"doc_id": "d2", "score": 3},
			},
			"claims": []map[string]any{
				{"doc_id": "d1", "paragraph": 2, "claim": "stated", "quote": datamarkedQuote},
			},
			"confidence": 0.5,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		rec, vs := Verify(string(b), docs)
		// Independent oracle: any URL in payload must be present in docs, and quote must resolve to quote after '^'->' '.
		wantLinks := AllowedLinks(string(b), docs)
		if len(wantLinks) == 0 {
			if len(vs) != 0 {
				t.Fatalf("unexpected violations %+v for quote %q", vs, quote)
			}
			if len(rec.Claims) != 1 || rec.Claims[0].Quote != quote || rec.Claims[0].QuoteSHA256 != shaHex(quote) {
				t.Fatalf("claim mismatch: got %+v want quote %q", rec.Claims, quote)
			}
		}
		if len(link) > len("https://") && strings.HasPrefix(link, "https://") && !strings.ContainsAny(link, " \t\n\r)^]>\"'") {
			lvs := AllowedLinks("check "+link+"^tail", docs)
			inDocs := strings.Contains(docs[0].Text, link) || strings.Contains(docs[1].Text, link)
			if !inDocs && len(lvs) == 0 {
				t.Fatalf("expected link violation for %q", link)
			}
		}
	})
}
