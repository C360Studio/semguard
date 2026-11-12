// Package untrusted segments, strips, datamarks, and frames untrusted source
// documents before they are handed to a reader model.
package untrusted

import (
	"crypto/sha256"
	_ "embed" // framing text
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Block is the datamarked document block string returned by Datamark.
type Block = string

// Document is one untrusted source text.
type Document struct {
	ID   string
	Text string
}

// Task is the trusted half of the prompt: what the reader is asked to do.
type Task struct {
	Instruction string
	Question    string
}

// Options holds the two frame switches and the size cap.
type Options struct {
	Datamark bool
	Framing  bool
	// MaxBytes caps len(System)+len(User); zero means DefaultMaxBytes.
	MaxBytes int
}

// Prompt is the reader's two chat messages. FramingSHA256 is empty when framing is off.
type Prompt struct {
	System        string
	User          string
	FramingSHA256 string
}

// Paragraph is one 1-based paragraph split on "\n\n" with its byte span in the segmented text.
type Paragraph struct {
	Index int
	Text  string
	Start int
	End   int
}

// OffsetMap maps byte offsets in a Strip-cleaned string back to byte offsets in the original text.
type OffsetMap struct {
	offsets []int
	origLen int
}

// OriginalOffset returns the byte offset in the original unstripped string corresponding
// to cleanByteOffset in the cleaned string. Offsets <= 0 return the start offset (or 0),
// and offsets >= len(cleaned) return the original string's byte length.
func (m OffsetMap) OriginalOffset(cleanByteOffset int) int {
	if len(m.offsets) == 0 {
		return 0
	}
	if cleanByteOffset < 0 {
		return m.offsets[0]
	}
	if cleanByteOffset >= len(m.offsets) {
		return m.origLen
	}
	return m.offsets[cleanByteOffset]
}

// Span maps a half-open byte range [cleanStart, cleanEnd) in the cleaned string
// back to the corresponding [origStart, origEnd) byte range in the original string.
// Because origEnd is OriginalOffset(cleanEnd), the returned span includes any characters
// stripped immediately after the byte at cleanEnd-1 up to the byte at cleanEnd (or len(text)).
func (m OffsetMap) Span(cleanStart, cleanEnd int) (origStart, origEnd int) {
	if cleanEnd < cleanStart {
		cleanEnd = cleanStart
	}
	return m.OriginalOffset(cleanStart), m.OriginalOffset(cleanEnd)
}

// DefaultMaxBytes is the prompt cap when Options.MaxBytes is zero.
// shortcut: byte cap, not tokens; upgrade when a reader model's context binds.
const DefaultMaxBytes = 16 << 10

//go:embed prompts/frame.txt
var framingText string

var framingSHA256 = func() string {
	s := sha256.Sum256([]byte(framingText))
	return hex.EncodeToString(s[:])
}()

var (
	idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	// wsRe matches the runes unicode.IsSpace reports, so no whitespace survives datamarking.
	wsRe = regexp.MustCompile(`[\s\v\x{85}\p{Z}]+`)
)

// Segment splits text on "\n\n" into 1-based Paragraphs with exact [Start, End) byte offsets.
func Segment(text string) []Paragraph {
	parts := strings.Split(text, "\n\n")
	out := make([]Paragraph, len(parts))
	offset := 0
	for i, p := range parts {
		start := offset
		end := start + len(p)
		out[i] = Paragraph{
			Index: i + 1,
			Text:  p,
			Start: start,
			End:   end,
		}
		offset = end + 2
	}
	return out
}

// Strip normalizes CRLF and CR line endings to LF, drops invalid UTF-8 bytes, removes
// Unicode format/zero-width characters (Cf) and control characters (Cc, except '\n' and '\t'),
// and removes reserved datamark tokens ("^", "<<report", "<</report") in a single O(n) pass
// so the cleaned text is guaranteed safe for Datamark and Frame. It returns the cleaned
// string and an OffsetMap mapping every byte index in the cleaned string back to text.
func Strip(text string) (string, OffsetMap) {
	out := make([]byte, 0, len(text))
	offsets := make([]int, 0, len(text)+1)

	for i := 0; i < len(text); {
		// Normalize \r\n -> \n and standalone \r -> \n.
		if text[i] == '\r' {
			out = append(out, '\n')
			offsets = append(offsets, i)
			if i+1 < len(text) && text[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			continue
		}
		// Strip reserved datamark tokens ("^", "<<report", "<</report").
		if text[i] == '^' {
			i++
			continue
		}
		if strings.HasPrefix(text[i:], "<</report") {
			i += len("<</report")
			continue
		}
		if strings.HasPrefix(text[i:], "<<report") {
			i += len("<<report")
			continue
		}

		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			// Drop invalid UTF-8 bytes so downstream JSON/UTF-8 handling is clean.
			i++
			continue
		}
		if isStrippedRune(r) {
			i += size
			continue
		}
		for b := 0; b < size; b++ {
			out = append(out, text[i+b])
			offsets = append(offsets, i+b)
		}
		out, offsets = trimMarkerSuffix(out, offsets)
		i += size
	}

	offsets = append(offsets, len(text))
	return string(out), OffsetMap{offsets: offsets, origLen: len(text)}
}

func isStrippedRune(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)
}

// trimMarkerSuffix truncates out and offsets if appending a kept ASCII rune completed
// a "<<report" or "<</report" token (e.g., from "<<^report" or "<<<<reportreport").
func trimMarkerSuffix(out []byte, offsets []int) ([]byte, []int) {
	const (
		openMarker  = "<<report"
		closeMarker = "<</report"
	)
	if n := len(out); n >= len(closeMarker) && string(out[n-len(closeMarker):]) == closeMarker {
		return out[:n-len(closeMarker)], offsets[:n-len(closeMarker)]
	}
	if n := len(out); n >= len(openMarker) && string(out[n-len(openMarker):]) == openMarker {
		return out[:n-len(openMarker)], offsets[:n-len(openMarker)]
	}
	return out, offsets
}

// check refuses a document that could forge or close a marker, or whose marks could not be undone.
func check(d Document) error {
	if !idRe.MatchString(d.ID) {
		return fmt.Errorf("document %q: id must match %s", d.ID, idRe)
	}
	for _, bad := range []string{"<<report", "<</report", "^"} {
		if strings.Contains(d.Text, bad) {
			return fmt.Errorf("document %s: text contains %q", d.ID, bad)
		}
	}
	return nil
}

// label prefixes each "\n\n" paragraph of text with "[n] ", its 1-based index as Segment and verify count it,
// after applying mark to the paragraph. The reader cites that n; without it the model has to count, and measured
// claims were off by one (#28).
func label(text string, mark func(string) string) string {
	paras := strings.Split(text, "\n\n")
	for i, p := range paras {
		paras[i] = "[" + strconv.Itoa(i+1) + "] " + mark(p)
	}
	return strings.Join(paras, "\n\n")
}

// Datamark wraps each document in <<report id=ID>> ... <</report>>, replaces every whitespace run inside a
// paragraph with ^, and labels each paragraph "[n] ". Paragraph breaks (\n\n) stay, so labels match the indices
// verify checks. The label's space cannot come from document text, whose whitespace is all ^.
func Datamark(docs []Document) (Block, error) {
	var b strings.Builder
	for i, d := range docs {
		if err := check(d); err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("<<report id=" + d.ID + ">>\n")
		b.WriteString(label(d.Text, func(p string) string { return wsRe.ReplaceAllString(p, "^") }))
		b.WriteString("\n<</report>>")
	}
	return b.String(), nil
}

// Unmark undoes datamarking on quoted text. It is exact when the source used single spaces inside paragraphs.
func Unmark(s string) string { return strings.ReplaceAll(s, "^", " ") }

// Frame assembles the reader prompt. It never truncates: over the cap is an error.
func Frame(t Task, docs []Document, o Options) (Prompt, error) {
	p := Prompt{System: t.Instruction}
	if o.Framing {
		p.System += "\n\n" + framingText
		p.FramingSHA256 = framingSHA256
	}
	var body string
	if o.Datamark {
		var err error
		if body, err = Datamark(docs); err != nil {
			return Prompt{}, err
		}
	} else {
		parts := make([]string, len(docs))
		for i, d := range docs {
			if err := check(d); err != nil {
				return Prompt{}, err
			}
			parts[i] = "Document " + d.ID + ":\n" + label(d.Text, func(p string) string { return p })
		}
		body = strings.Join(parts, "\n\n")
	}
	p.User = "Question: " + t.Question + "\n\n" + body
	limit := o.MaxBytes
	if limit == 0 {
		limit = DefaultMaxBytes
	}
	if n := len(p.System) + len(p.User); n > limit {
		return Prompt{}, fmt.Errorf("prompt is %d bytes, cap is %d", n, limit)
	}
	return p, nil
}
