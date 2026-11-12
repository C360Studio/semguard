// Package verify validates reader outputs against task schemas, resolves
// paragraph-grounded claims, and enforces the document link allowlist.
package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Kind identifies the category of a verification violation.
type Kind string

const (
	// KindSchema marks a JSON syntax, unknown-field, or task-schema constraint failure (B1).
	KindSchema Kind = "schema"
	// KindClaim marks a claim whose doc_id, paragraph index, quote, or quote_sha256 fails to resolve (B1).
	KindClaim Kind = "claim"
	// KindLink marks a URL in the output that does not appear byte-for-byte in any input document (H2 / B1).
	KindLink Kind = "link"
)

// Document is one source text shown to the reader.
type Document struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Score is the reader's 0..10 score for one document.
type Score struct {
	DocID string `json:"doc_id"`
	Score int    `json:"score"`
}

// Claim is one paragraph-grounded statement extracted by the reader.
type Claim struct {
	DocID       string `json:"doc_id"`
	Paragraph   int    `json:"paragraph"`
	Claim       string `json:"claim"`
	Quote       string `json:"quote"`
	QuoteSHA256 string `json:"quote_sha256"`
}

// Record is the verified reader output for the score-compare task.
type Record struct {
	Scores     []Score `json:"scores"`
	Claims     []Claim `json:"claims"`
	Confidence float64 `json:"confidence"`
}

// Violation describes one schema, claim, or link failure in a reader output.
type Violation struct {
	Kind      Kind   `json:"kind"`
	DocID     string `json:"doc_id,omitempty"`
	Paragraph int    `json:"paragraph,omitempty"`
	URL       string `json:"url,omitempty"`
	Message   string `json:"message"`
}

func (v Violation) Error() string { return v.Message }

// urlRe matches URLs in output text, stopping at whitespace, closing delimiters, datamark '^', and a markdown
// backtick (#44: a reader that writes a document URL as inline code must not trip H2), and typographic quotes
// U+201C U+201D U+2018 U+2019 (#46: a reader that quotes a document URL typographically must not trip H2).
var urlRe = regexp.MustCompile("(?i)(?:[a-z][a-z0-9+.-]*://|\\bwww\\.)[^\\s)\\]>\"'^`\\x{201C}\\x{201D}\\x{2018}\\x{2019}]+")

var (
	rootAllowedKeys  = map[string]bool{"scores": true, "claims": true, "confidence": true}
	scoreAllowedKeys = map[string]bool{"doc_id": true, "score": true}
	claimAllowedKeys = map[string]bool{"doc_id": true, "paragraph": true, "claim": true, "quote": true, "quote_sha256": true}
)

// rawWire is the strict JSON wire structure for score-compare.
type rawWire struct {
	Scores     *[]rawScore `json:"scores"`
	Claims     *[]rawClaim `json:"claims"`
	Confidence *float64    `json:"confidence"`
}

type rawScore struct {
	DocID *string `json:"doc_id"`
	Score *int    `json:"score"`
}

type rawClaim struct {
	DocID       *string `json:"doc_id"`
	Paragraph   *int    `json:"paragraph"`
	Claim       *string `json:"claim"`
	Quote       *string `json:"quote"`
	QuoteSHA256 *string `json:"quote_sha256"`
}

// Verify validates raw against the score-compare schema, resolves every claim's
// paragraph index and quote/quote_sha256 against docs, and checks AllowedLinks.
// Any violation causes the output to be rejected (empty Record returned alongside all violations).
func Verify(raw string, docs []Document) (Record, []Violation) {
	var vs []Violation
	addSchema := func(format string, a ...any) {
		vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf(format, a...)})
	}

	var w rawWire
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		addSchema("invalid JSON: %v", err)
		vs = append(vs, AllowedLinks(raw, docs)...)
		return Record{}, vs
	}
	if _, err := dec.Token(); err != io.EOF {
		addSchema("trailing data after JSON object")
		vs = append(vs, AllowedLinks(raw, docs)...)
		return Record{}, vs
	}
	vs = append(vs, checkExactKeys(raw)...)

	if w.Scores == nil {
		addSchema("missing required field \"scores\"")
	}
	if w.Claims == nil {
		addSchema("missing required field \"claims\"")
	}
	if w.Confidence == nil {
		addSchema("missing required field \"confidence\"")
	} else if math.IsNaN(*w.Confidence) || *w.Confidence < 0 || *w.Confidence > 1 {
		addSchema("confidence %v outside 0..1", *w.Confidence)
	}

	docMap := make(map[string]Document, len(docs))
	for _, d := range docs {
		docMap[d.ID] = d
	}

	var rec Record
	if w.Confidence != nil {
		rec.Confidence = *w.Confidence
	}

	seenScores := make(map[string]bool, len(docs))
	if w.Scores != nil {
		for i, s := range *w.Scores {
			if s.DocID == nil || *s.DocID == "" {
				addSchema("scores[%d]: missing doc_id", i)
				continue
			}
			if s.Score == nil {
				addSchema("scores[%d] (%s): missing score", i, *s.DocID)
				continue
			}
			id := *s.DocID
			sc := *s.Score
			if _, ok := docMap[id]; !ok {
				addSchema("scores[%d]: unknown doc_id %q", i, id)
			}
			if seenScores[id] {
				addSchema("scores[%d]: duplicate score for doc_id %q", i, id)
			}
			seenScores[id] = true
			if sc < 0 || sc > 10 {
				addSchema("scores[%d] (%s): score %d outside 0..10", i, id, sc)
			}
			rec.Scores = append(rec.Scores, Score{DocID: id, Score: sc})
		}
		for _, d := range docs {
			if !seenScores[d.ID] {
				addSchema("missing score for document %q", d.ID)
			}
		}
	}

	if w.Claims != nil {
		for i, c := range *w.Claims {
			claim, cv := verifyClaim(i, c, docMap)
			if len(cv) > 0 {
				vs = append(vs, cv...)
				continue
			}
			rec.Claims = append(rec.Claims, claim)
		}
	}

	vs = append(vs, AllowedLinks(raw, docs)...)
	if len(vs) > 0 {
		return Record{}, vs
	}
	return rec, nil
}

// checkExactKeys enforces case-sensitive JSON property names and rejects duplicate object keys
// at the root, scores[], and claims[] levels (since encoding/json matches struct tags case-insensitively).
func checkExactKeys(raw string) []Violation {
	var vs []Violation
	rootFields, err := parseObjectFields([]byte(raw))
	if err != nil {
		return nil
	}
	seenRoot := make(map[string]bool, len(rootFields))
	for _, f := range rootFields {
		if !rootAllowedKeys[f.key] {
			vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("unknown field %q", f.key)})
			continue
		}
		if seenRoot[f.key] {
			vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("duplicate field %q", f.key)})
		}
		seenRoot[f.key] = true
		switch f.key {
		case "scores":
			var elems []json.RawMessage
			if err := json.Unmarshal(f.val, &elems); err == nil {
				for i, el := range elems {
					vs = append(vs, checkSubObjectKeys(fmt.Sprintf("scores[%d]", i), el, scoreAllowedKeys)...)
				}
			}
		case "claims":
			var elems []json.RawMessage
			if err := json.Unmarshal(f.val, &elems); err == nil {
				for i, el := range elems {
					vs = append(vs, checkSubObjectKeys(fmt.Sprintf("claims[%d]", i), el, claimAllowedKeys)...)
				}
			}
		}
	}
	return vs
}

func checkSubObjectKeys(label string, raw json.RawMessage, allowed map[string]bool) []Violation {
	var vs []Violation
	fields, err := parseObjectFields(raw)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !allowed[f.key] {
			vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("%s: unknown field %q", label, f.key)})
			continue
		}
		if seen[f.key] {
			vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("%s: duplicate field %q", label, f.key)})
		}
		seen[f.key] = true
	}
	return vs
}

type rawField struct {
	key string
	val json.RawMessage
}

func parseObjectFields(b []byte) ([]rawField, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not an object")
	}
	var fields []rawField
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, fmt.Errorf("non-string key")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		fields = append(fields, rawField{key: key, val: val})
	}
	return fields, nil
}

func verifyClaim(idx int, c rawClaim, docMap map[string]Document) (Claim, []Violation) {
	var vs []Violation
	if c.DocID == nil || *c.DocID == "" {
		vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("claims[%d]: missing doc_id", idx)})
	}
	if c.Paragraph == nil {
		vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("claims[%d]: missing paragraph", idx)})
	}
	if c.Claim == nil || strings.TrimSpace(*c.Claim) == "" {
		vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("claims[%d]: missing claim text", idx)})
	}
	if c.Quote == nil || *c.Quote == "" {
		vs = append(vs, Violation{Kind: KindSchema, Message: fmt.Sprintf("claims[%d]: missing quote", idx)})
	}
	if len(vs) > 0 {
		return Claim{}, vs
	}

	docID := *c.DocID
	paraIdx := *c.Paragraph
	normQuote := strings.ReplaceAll(*c.Quote, "^", " ")
	sum := sha256.Sum256([]byte(normQuote))
	wantHash := hex.EncodeToString(sum[:])

	doc, ok := docMap[docID]
	if !ok {
		return Claim{}, []Violation{{
			Kind:      KindClaim,
			DocID:     docID,
			Paragraph: paraIdx,
			Message:   fmt.Sprintf("claims[%d]: unknown doc_id %q", idx, docID),
		}}
	}

	paras := strings.Split(doc.Text, "\n\n")
	if paraIdx < 1 || paraIdx > len(paras) {
		return Claim{}, []Violation{{
			Kind:      KindClaim,
			DocID:     docID,
			Paragraph: paraIdx,
			Message:   fmt.Sprintf("claims[%d] (%s): paragraph %d outside 1..%d", idx, docID, paraIdx, len(paras)),
		}}
	}

	paraText := paras[paraIdx-1]
	if !strings.Contains(paraText, normQuote) {
		return Claim{}, []Violation{{
			Kind:      KindClaim,
			DocID:     docID,
			Paragraph: paraIdx,
			Message:   fmt.Sprintf("claims[%d] (%s paragraph %d): quote not found in paragraph", idx, docID, paraIdx),
		}}
	}

	if c.QuoteSHA256 != nil && *c.QuoteSHA256 != "" {
		given := strings.ToLower(*c.QuoteSHA256)
		if given != wantHash {
			return Claim{}, []Violation{{
				Kind:      KindClaim,
				DocID:     docID,
				Paragraph: paraIdx,
				Message:   fmt.Sprintf("claims[%d] (%s paragraph %d): quote_sha256 %q does not match normalized quote sha256 %s", idx, docID, paraIdx, *c.QuoteSHA256, wantHash),
			}}
		}
	}

	return Claim{
		DocID:       docID,
		Paragraph:   paraIdx,
		Claim:       *c.Claim,
		Quote:       normQuote,
		QuoteSHA256: wantHash,
	}, nil
}

// AllowedLinks flags every URL in output that does not appear byte-for-byte in at least one document (H2).
// Every valid JSON string literal in output is decoded in place before scanning so JSON escape sequences
// (such as \/ or \u0026) neither hide unallowed URLs nor false-flag allowed URLs, while preserving all
// surrounding prose (outside code fences) and duplicate JSON object members.
func AllowedLinks(output string, docs []Document) []Violation {
	var vs []Violation
	for _, u := range extractURLs(output) {
		found := false
		for _, d := range docs {
			if strings.Contains(d.Text, u) {
				found = true
				break
			}
		}
		if !found {
			vs = append(vs, Violation{
				Kind:    KindLink,
				URL:     u,
				Message: fmt.Sprintf("unallowed URL %q absent from all documents", u),
			})
		}
	}
	return vs
}

var jsonStringLiteralRe = regexp.MustCompile(`"(?:\\.|[^"\\\r\n])*"`)

func extractURLs(output string) []string {
	decoded := jsonStringLiteralRe.ReplaceAllStringFunc(output, func(lit string) string {
		var s string
		if err := json.Unmarshal([]byte(lit), &s); err == nil {
			return `"` + s + `"`
		}
		return lit
	})
	return urlRe.FindAllString(decoded, -1)
}

// ParseFree leniently extracts per-document 0..10 scores from a free-mode completion
// (either fenced/unfenced JSON or plain "doc_id: score" text) so H3 rank comparisons can run.
func ParseFree(raw string, docs []Document) Record {
	known := make(map[string]bool, len(docs))
	for _, d := range docs {
		known[d.ID] = true
	}

	trimmed := stripFences(raw)
	var loose struct {
		Scores     []Score `json:"scores"`
		Claims     []Claim `json:"claims"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(trimmed), &loose); err == nil && len(loose.Scores) > 0 {
		var filtered []Score
		seen := make(map[string]bool, len(docs))
		for _, s := range loose.Scores {
			if known[s.DocID] && !seen[s.DocID] && s.Score >= 0 && s.Score <= 10 {
				seen[s.DocID] = true
				filtered = append(filtered, s)
			}
		}
		if len(filtered) > 0 {
			loose.Scores = filtered
			return Record{Scores: loose.Scores, Claims: loose.Claims, Confidence: loose.Confidence}
		}
	}

	// Fallback: scan for "<doc_id> ... <0..10>" per known document ID. Bracketed spans are blanked first so a
	// cited "[n]" paragraph label is never read as a score; a score that is then out of reach stays missing.
	text := blankBrackets(raw)
	var scores []Score
	for _, d := range docs {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(d.ID) + `\b[^\n0-9]{0,24}\b(10|[0-9])\b`)
		if m := re.FindStringSubmatch(text); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				scores = append(scores, Score{DocID: d.ID, Score: n})
			}
		}
	}
	return Record{Scores: scores}
}

// blankBrackets replaces every character inside [...] with a space, keeping the brackets and newlines so line
// boundaries hold. Spans nest and may cross lines; an unclosed "[" blanks the rest of the text (a score there stays
// missing, never guessed), and a stray "]" is kept as is.
func blankBrackets(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
			b.WriteRune(r)
		case r == ']':
			if depth > 0 {
				depth--
			}
			b.WriteRune(r)
		case depth > 0 && r != '\n':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripFences(s string) string {
	b := bytes.TrimSpace([]byte(s))
	if bytes.HasPrefix(b, []byte("```")) {
		if idx := bytes.IndexByte(b, '\n'); idx >= 0 {
			b = b[idx+1:]
		}
		if end := bytes.LastIndex(b, []byte("```")); end >= 0 {
			b = b[:end]
		}
	}
	return string(bytes.TrimSpace(b))
}
