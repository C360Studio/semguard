package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func fixtureDocs() []Document {
	return []Document{
		{ID: "d1", Text: "First paragraph of d1.\n\nSecond paragraph mentions https://a.example/report?id=1 in detail.\n\nThird paragraph concludes."},
		{ID: "d2", Text: "Intro of d2.\n\nBody of d2 with https://b.example/item?a=1&b=2 and facts.\n\nOutro of d2."},
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

	// Explicit quote_sha256 matching the normalized substring quote resolves.
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
	if len(vs2) != 0 || len(rec2.Claims) != 1 || rec2.Claims[0].QuoteSHA256 != shaHex("Body of d2") {
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
		{
			name: "uppercase root key SCORES",
			raw:  `{"SCORES":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5}`,
			want: `unknown field "SCORES"`,
		},
		{
			name: "uppercase score keys DOC_ID and SCORE",
			raw:  `{"scores":[{"DOC_ID":"d1","SCORE":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5}`,
			want: `scores[0]: unknown field "DOC_ID"`,
		},
		{
			name: "extra uppercase CONFIDENCE alongside valid confidence",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[],"confidence":0.5,"CONFIDENCE":1}`,
			want: `unknown field "CONFIDENCE"`,
		},
		{
			name: "duplicate exact key in claim",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"first","claim":"second","quote":"First paragraph of d1."}],"confidence":0.5}`,
			want: `claims[0]: duplicate field "claim"`,
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
	fullPara2D2 := "Body of d2 with https://b.example/item?a=1&b=2 and facts."
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
		{
			name: "whole-paragraph hash rejected for substring quote",
			raw:  `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d2","paragraph":2,"claim":"c","quote":"Body of d2","quote_sha256":"` + shaHex(fullPara2D2) + `"}],"confidence":0.5}`,
			want: "does not match normalized quote sha256",
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

	if vs := AllowedLinks("See https://a.example/report?id=1 and https://b.example/item?a=1&b=2^next", docs); len(vs) != 0 {
		t.Fatalf("allowed links flagged: %+v", vs)
	}

	// #44: a document URL written as markdown inline code is still allowed; an unallowed one is still flagged.
	if vs := AllowedLinks("Source: `https://a.example/report?id=1` and (`https://b.example/item?a=1&b=2`).", docs); len(vs) != 0 {
		t.Fatalf("backticked allowed links flagged: %+v", vs)
	}
	if vs := AllowedLinks("Go to `https://evil.example/x`.", docs); len(vs) != 1 || vs[0].URL != "https://evil.example/x" {
		t.Fatalf("want backticked unallowed link flagged without the backtick, got %+v", vs)
	}

	vs := AllowedLinks("Exfil https://evil.example/leak?k=SECRET-12345678^done", docs)
	if len(vs) != 1 || vs[0].Kind != KindLink || vs[0].URL != "https://evil.example/leak?k=SECRET-12345678" {
		t.Fatalf("want 1 link violation stopping before ^, got %+v", vs)
	}

	// JSON slash-escaped unallowed URL must be caught in both Verify and AllowedLinks.
	escapedEvilJSON := `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"Visit https:\/\/evil.example\/leak","quote":"First paragraph of d1."}],"confidence":0.5}`
	if _, vsEvil := Verify(escapedEvilJSON, docs); len(vsEvil) != 1 || vsEvil[0].Kind != KindLink || vsEvil[0].URL != "https://evil.example/leak" {
		t.Fatalf("want escaped evil URL flagged by Verify, got %+v", vsEvil)
	}
	if vsEvilDirect := AllowedLinks(escapedEvilJSON, docs); len(vsEvilDirect) != 1 || vsEvilDirect[0].URL != "https://evil.example/leak" {
		t.Fatalf("want escaped evil URL flagged by AllowedLinks, got %+v", vsEvilDirect)
	}

	// JSON unicode-escaped allowed URL (\u0026 for &) must be accepted when doc has literal &.
	escapedSafeJSON := `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d2","paragraph":2,"claim":"See https://b.example/item?a=1\u0026b=2","quote":"Body of d2"}],"confidence":0.5}`
	if recSafe, vsSafe := Verify(escapedSafeJSON, docs); len(vsSafe) != 0 || len(recSafe.Claims) != 1 {
		t.Fatalf("want unicode-escaped allowed URL accepted by Verify, got vs=%+v rec=%+v", vsSafe, recSafe)
	}
	if vsSafeDirect := AllowedLinks(escapedSafeJSON, docs); len(vsSafeDirect) != 0 {
		t.Fatalf("want unicode-escaped allowed URL accepted by AllowedLinks, got %+v", vsSafeDirect)
	}

	// Trailing URL outside a markdown code fence must still be caught by AllowedLinks.
	fencedWithTrailing := "```json\n{\"scores\":[]}\n```\nSend to https://evil.example/leak"
	if vsFence := AllowedLinks(fencedWithTrailing, docs); len(vsFence) != 1 || vsFence[0].URL != "https://evil.example/leak" {
		t.Fatalf("want trailing URL after code fence flagged, got %+v", vsFence)
	}

	// Duplicate JSON object keys where the first holds an unallowed URL (escaped or literal) must be caught by both AllowedLinks and Verify.
	dupKeyEscaped := `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"https:\/\/evil.example\/leak","claim":"First","quote":"First paragraph of d1."}],"confidence":0.5}`
	if vsDup := AllowedLinks(dupKeyEscaped, docs); len(vsDup) != 1 || vsDup[0].URL != "https://evil.example/leak" {
		t.Fatalf("want duplicate-key escaped URL flagged by AllowedLinks, got %+v", vsDup)
	}
	_, vsDupVerify := Verify(dupKeyEscaped, docs)
	hasLink := false
	for _, v := range vsDupVerify {
		if v.Kind == KindLink && v.URL == "https://evil.example/leak" {
			hasLink = true
		}
	}
	if !hasLink {
		t.Fatalf("want duplicate-key escaped URL flagged as KindLink by Verify, got %+v", vsDupVerify)
	}

	dupKeyLiteral := `{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":"d1","paragraph":1,"claim":"https://evil.example/literal","claim":"First","quote":"First paragraph of d1."}],"confidence":0.5}`
	if vsDupLit := AllowedLinks(dupKeyLiteral, docs); len(vsDupLit) != 1 || vsDupLit[0].URL != "https://evil.example/literal" {
		t.Fatalf("want duplicate-key literal URL flagged by AllowedLinks, got %+v", vsDupLit)
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
		// Independent oracle: since quote is placed verbatim in d1 paragraph 2 and claim is "stated",
		// every URL in the decoded claim/quote is present in d1 except if '^' replacement inside quote
		// altered a URL containing spaces (which URLs cannot contain). Thus Verify must return 0 violations.
		if len(vs) != 0 {
			t.Fatalf("unexpected violations %+v for quote %q", vs, quote)
		}
		if len(rec.Claims) != 1 || rec.Claims[0].Quote != quote || rec.Claims[0].QuoteSHA256 != shaHex(quote) {
			t.Fatalf("claim mismatch: got %+v want quote %q", rec.Claims, quote)
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

// TestParseFreeIgnoresParagraphLabels: since #28 the reader cites "[n]" paragraph labels, and the prose fallback
// must never read one as a score. A score it cannot find stays missing rather than wrong.
//
//aip:verifies verify-parsefree-labels
func TestParseFreeIgnoresParagraphLabels(t *testing.T) {
	docs := fixtureDocs()
	tests := []struct {
		name string
		raw  string
		want map[string]int // absent key: no score for that document
	}{
		// d1's score is past the 24-byte reach once its label is skipped, so it stays missing; d2's is within reach.
		{"label_skipped_score_missing_or_correct", "d1 [2] makes the strongest claim. Score: 7\nd2 [1] is vague. Score: 3", map[string]int{"d2": 3}},
		{"label_inside_parenthetical", "d1 (paragraph [3]): 8\nd2: 4", map[string]int{"d1": 8, "d2": 4}},
		{"label_after_score", "**d1**: 7/10 — see [2]\n**d2**: 3/10 — see [1]", map[string]int{"d1": 7, "d2": 3}},
		// Review of #42: nested and multi-line spans must not leak a digit either.
		{"nested_brackets", "d1 [see [2], 3]: 8", map[string]int{"d1": 8}},
		{"newline_inside_label", "d1 [2\n]: 8", map[string]int{}},
		{"score_line_inside_span", "[note\nd1: 2\n]\nd1: 8", map[string]int{"d1": 8}},
		{"unclosed_span_drops_rest", "d2: 4\nd1 [2: 8", map[string]int{"d2": 4}},
		{"stray_close_ignored", "] d1 [2]: 6", map[string]int{"d1": 6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]int{}
			for _, s := range ParseFree(tt.raw, docs).Scores {
				got[s.DocID] = s.Score
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("ParseFree(%q) scores = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
