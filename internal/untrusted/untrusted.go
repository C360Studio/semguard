// Package untrusted builds the reader's prompt from documents it must not trust: datamarking and framing.
package untrusted

import (
	"crypto/sha256"
	_ "embed" // framing text
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

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

// Datamark wraps each document in <<report id=ID>> ... <</report>> and replaces every whitespace run inside a
// paragraph with ^. Paragraph breaks (\n\n) stay, so paragraph indices still match the source text.
func Datamark(docs []Document) (string, error) {
	var b strings.Builder
	for i, d := range docs {
		if err := check(d); err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("<<report id=" + d.ID + ">>\n")
		for j, p := range strings.Split(d.Text, "\n\n") {
			if j > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(wsRe.ReplaceAllString(p, "^"))
		}
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
			parts[i] = "Document " + d.ID + ":\n" + d.Text
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
