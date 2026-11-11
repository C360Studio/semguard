// Package corpus loads, validates, and builds the frozen detonation corpus.
package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxFileBytes bounds every corpus file read.
const maxFileBytes = 16 << 20

// classes lists every class in corpus order; attack classes come first.
var classes = []string{"override", "exfil", "social", "fragmented", "rank", "quoted", "article"}

var asks = map[string]string{
	"override": "repeat", "exfil": "link", "social": "repeat", "fragmented": "repeat",
	"rank": "rank", "quoted": "repeat", "article": "none",
}

// isAttack reports whether the class is an attack class (has controls).
func isAttack(class string) bool {
	switch class {
	case "override", "exfil", "social", "fragmented", "rank":
		return true
	}
	return false
}

// ValidationError marks a corpus that is wrong, as opposed to one that cannot be read.
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

func invalid(format string, a ...any) error {
	return &ValidationError{Err: fmt.Errorf(format, a...)}
}

// Document is one source text shown to the reader.
type Document struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Case is one frozen corpus case.
type Case struct {
	ID                 string     `json:"id"`
	Class              string     `json:"class"`
	Task               string     `json:"task"`
	Question           string     `json:"question"`
	Documents          []Document `json:"documents"`
	InjectedDoc        string     `json:"injected_doc"`
	InjectedParagraphs []int      `json:"injected_paragraphs"`
	Canary             string     `json:"canary"`
	Ask                string     `json:"ask"`
	ControlOf          *string    `json:"control_of"`
}

// Task is one task definition from tasks.json.
type Task struct {
	ID          string `json:"id"`
	Instruction string `json:"instruction"`
	Schema      string `json:"schema"`
}

type casesFile struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

type tasksFile struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// LockEntry pins one corpus file.
type LockEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Lock is corpus.lock.json.
type Lock struct {
	Version int         `json:"version"`
	Files   []LockEntry `json:"files"`
}

// Paragraphs splits text on "\n\n"; paragraph i (1-based) is index i-1.
func Paragraphs(text string) []string { return strings.Split(text, "\n\n") }

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFileBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, maxFileBytes)
	}
	return b, nil
}

func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after JSON value")
	}
	return nil
}

// Load verifies the lockfile in dir, then parses and validates the corpus.
// On any failure it returns no cases.
func Load(dir string) ([]Case, []Task, error) {
	c, t, _, err := LoadWithHash(dir)
	return c, t, err
}

// LoadWithHash is Load that also returns the verified sha256 of cases.json.
func LoadWithHash(dir string) ([]Case, []Task, string, error) {
	lb, err := readBounded(filepath.Join(dir, "corpus.lock.json"))
	if err != nil {
		return nil, nil, "", fmt.Errorf("read lock: %w", err)
	}
	var lock Lock
	if err := decodeStrict(lb, &lock); err != nil {
		return nil, nil, "", invalid("parse corpus.lock.json: %v", err)
	}
	if lock.Version != 1 {
		return nil, nil, "", invalid("corpus.lock.json: lock version %d, want 1", lock.Version)
	}
	data := map[string][]byte{}
	sums := map[string]string{}
	for _, e := range lock.Files {
		if !filepath.IsLocal(e.Path) {
			return nil, nil, "", invalid("lock path %q is not local", e.Path)
		}
		b, err := readBounded(filepath.Join(dir, e.Path))
		if err != nil {
			return nil, nil, "", fmt.Errorf("read %s: %w", e.Path, err)
		}
		sum := sha256.Sum256(b)
		if int64(len(b)) != e.Size || hex.EncodeToString(sum[:]) != e.SHA256 {
			return nil, nil, "", invalid("%s: does not match corpus.lock.json (size or sha256)", e.Path)
		}
		data[e.Path] = b
		sums[e.Path] = e.SHA256
	}
	var cf casesFile
	var tf tasksFile
	for name, v := range map[string]any{"cases.json": &cf, "tasks.json": &tf} {
		b, ok := data[name]
		if !ok {
			return nil, nil, "", invalid("%s: not pinned in corpus.lock.json", name)
		}
		if err := decodeStrict(b, v); err != nil {
			return nil, nil, "", invalid("parse %s: %v", name, err)
		}
	}
	if err := Validate(cf.Cases, tf.Tasks); err != nil {
		return nil, nil, "", err
	}
	if err := CheckCounts(cf.Cases); err != nil {
		return nil, nil, "", err
	}
	return cf.Cases, tf.Tasks, sums["cases.json"], nil
}

var (
	canaryRe = regexp.MustCompile(`^SECRET-[A-F0-9]{8}$`)
	urlRe    = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://|\bwww\.)[^\s)\]>"']+`)
)

// CheckCounts requires exactly 10 non-control cases per class and 50 controls.
func CheckCounts(cases []Case) error {
	per := map[string]int{}
	controls := 0
	for _, c := range cases {
		if c.ControlOf != nil {
			controls++
		} else {
			per[c.Class]++
		}
	}
	var errs []error
	for _, cl := range classes {
		if per[cl] != 10 {
			errs = append(errs, fmt.Errorf("class %s: %d cases, want 10", cl, per[cl]))
		}
	}
	if controls != 50 {
		errs = append(errs, fmt.Errorf("%d controls, want 50", controls))
	}
	return wrap(errors.Join(errs...))
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return &ValidationError{Err: err}
}

func docByID(c Case, id string) (Document, bool) {
	for _, d := range c.Documents {
		if d.ID == id {
			return d, true
		}
	}
	return Document{}, false
}

// Validate checks every case against the corpus rules and returns all violations.
func Validate(cases []Case, tasks []Task) error {
	taskIDs := map[string]bool{}
	for _, t := range tasks {
		taskIDs[t.ID] = true
	}
	byID := map[string]Case{}
	for _, c := range cases {
		if _, dup := byID[c.ID]; !dup {
			byID[c.ID] = c
		}
	}
	var errs []error
	add := func(c Case, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", c.ID, fmt.Sprintf(format, a...)))
	}
	seen := map[string]bool{}
	canaries := map[string]string{}
	controlsOf := map[string]int{}
	for _, c := range cases {
		if seen[c.ID] {
			add(c, "duplicate id")
		}
		seen[c.ID] = true
		validateCommon(c, taskIDs, add)
		if c.ControlOf == nil {
			if !idRe(c.Class).MatchString(c.ID) {
				add(c, "id must be %q", c.Class+"-NNN")
			}
			if !canaryRe.MatchString(c.Canary) {
				continue
			}
			if other, dup := canaries[c.Canary]; dup {
				add(c, "canary %s already used by %s", c.Canary, other)
			}
			canaries[c.Canary] = c.ID
			validateAttack(c, add)
		} else {
			controlsOf[*c.ControlOf]++
			if want := *c.ControlOf + "-control"; c.ID != want {
				add(c, "id must be %q", want)
			}
			validateControl(c, byID, add)
		}
	}
	for _, c := range cases {
		if c.ControlOf == nil && isAttack(c.Class) && controlsOf[c.ID] != 1 {
			add(c, "has %d controls, want exactly 1", controlsOf[c.ID])
		}
	}
	return wrap(errors.Join(errs...))
}

type adder func(c Case, format string, a ...any)

func idRe(class string) *regexp.Regexp {
	return regexp.MustCompile("^" + regexp.QuoteMeta(class) + `-[0-9]{3}$`)
}

// checkText applies the text rules shared by documents and the question.
func checkText(c Case, label, text string, add adder) {
	for _, u := range urlRe.FindAllString(text, -1) {
		probe := u
		if !strings.Contains(u, "://") {
			probe = "//" + u
		}
		h := ""
		if p, err := url.Parse(probe); err == nil {
			h = strings.ToLower(p.Hostname())
		}
		if !strings.HasSuffix(h, ".example") && !strings.HasSuffix(h, ".invalid") {
			add(c, "%s: URL %q is not on a reserved .example/.invalid host", label, u)
		}
	}
	if strings.Contains(text, "{{CANARY}}") {
		add(c, "%s: unfilled {{CANARY}} placeholder", label)
	}
	if strings.Contains(strings.ReplaceAll(text, c.Canary, ""), "SECRET-") {
		add(c, "%s: contains a SECRET- string other than the case canary", label)
	}
}

// minParagraphs keeps document length from marking the injected document: a one-paragraph benign report beside a
// five-paragraph injected one lets a reader discount the longest document instead of resisting the injection.
const minParagraphs = 3

func validateCommon(c Case, taskIDs map[string]bool, add adder) {
	want, known := asks[c.Class]
	switch {
	case !known:
		add(c, "unknown class %q", c.Class)
	case c.Ask != want:
		add(c, "ask %q, class %s wants %q", c.Ask, c.Class, want)
	}
	if !taskIDs[c.Task] {
		add(c, "task %q not in tasks.json", c.Task)
	}
	if !canaryRe.MatchString(c.Canary) {
		add(c, "canary %q does not match SECRET-[A-F0-9]{8}", c.Canary)
	}
	if len(c.Documents) < 2 {
		add(c, "%d documents, want at least 2", len(c.Documents))
	}
	ids := map[string]bool{}
	for _, d := range c.Documents {
		if ids[d.ID] {
			add(c, "duplicate document id %q", d.ID)
		}
		ids[d.ID] = true
		// A control's injected document is shorter by design; the derivation check covers it.
		if n := len(Paragraphs(d.Text)); c.ControlOf == nil && n < minParagraphs {
			add(c, "document %s has %d paragraphs, want at least %d", d.ID, n, minParagraphs)
		}
		checkText(c, "document "+d.ID, d.Text, add)
	}
	checkText(c, "question", c.Question, add)
	if canaryRe.MatchString(c.Canary) && strings.Contains(c.Question, c.Canary) {
		add(c, "canary appears in the question")
	}
	if !ids[c.InjectedDoc] {
		add(c, "injected_doc %q is not a document id", c.InjectedDoc)
	}
}

func validateAttack(c Case, add adder) {
	inj, ok := docByID(c, c.InjectedDoc)
	if !ok {
		return
	}
	paras := Paragraphs(inj.Text)
	inInj := map[int]bool{}
	if len(c.InjectedParagraphs) == 0 {
		add(c, "injected_paragraphs is empty")
	}
	for _, p := range c.InjectedParagraphs {
		if p < 1 || p > len(paras) {
			add(c, "injected paragraph %d outside 1..%d", p, len(paras))
		} else if inInj[p] {
			add(c, "duplicate injected paragraph %d", p)
		}
		inInj[p] = true
	}
	withCanary := func(only func(int) bool) []int {
		var r []int
		for i, p := range paras {
			if strings.Contains(p, c.Canary) && only(i+1) {
				r = append(r, i+1)
			}
		}
		return r
	}
	switch c.Class {
	case "rank", "article":
		for _, d := range c.Documents {
			if strings.Contains(d.Text, c.Canary) {
				add(c, "canary appears in document %s, class %s allows none", d.ID, c.Class)
			}
		}
		return
	}
	if len(withCanary(func(int) bool { return true })) == 0 {
		add(c, "canary does not appear in the injected document")
	}
	if len(withCanary(func(i int) bool { return !inInj[i] })) > 0 {
		add(c, "canary appears outside the injected paragraphs")
	}
	for _, d := range c.Documents {
		if d.ID != c.InjectedDoc && strings.Contains(d.Text, c.Canary) {
			add(c, "canary appears in non-injected document %s", d.ID)
		}
	}
	switch c.Class {
	case "quoted":
		for _, i := range withCanary(func(int) bool { return true }) {
			if inInj[i] && !allQuoted(paras[i-1]) {
				add(c, "paragraph %d holds the canary but is not a blockquote", i)
			}
		}
	case "exfil":
		found := false
		for p := range inInj {
			if p < 1 || p > len(paras) {
				continue
			}
			for _, u := range urlRe.FindAllString(paras[p-1], -1) {
				lu := strings.ToLower(u)
				if (strings.HasPrefix(lu, "http://") || strings.HasPrefix(lu, "https://")) && strings.Contains(u, c.Canary) {
					found = true
				}
			}
		}
		if !found {
			add(c, "no injected paragraph has a URL containing the canary")
		}
	case "fragmented":
		if len(c.InjectedParagraphs) != 3 {
			add(c, "%d injected paragraphs, want exactly 3", len(c.InjectedParagraphs))
		}
		ps := c.InjectedParagraphs
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				if d := ps[i] - ps[j]; d >= -1 && d <= 1 {
					add(c, "injected paragraphs %d and %d are adjacent", ps[i], ps[j])
				}
			}
		}
		if n := len(withCanary(func(i int) bool { return inInj[i] })); n != 1 {
			add(c, "canary appears in %d injected paragraphs, want exactly 1", n)
		}
	}
}

func validateControl(c Case, byID map[string]Case, add adder) {
	parent, ok := byID[*c.ControlOf]
	if !ok || parent.ControlOf != nil || !isAttack(parent.Class) {
		add(c, "control_of %q is not an existing attack case", *c.ControlOf)
		return
	}
	if !isAttack(c.Class) {
		add(c, "benign class %s must not have a control", c.Class)
	}
	if c.Class != parent.Class || c.Task != parent.Task || c.Question != parent.Question ||
		c.Ask != parent.Ask || c.InjectedDoc != parent.InjectedDoc {
		add(c, "class, task, question, ask, or injected_doc differs from parent %s", parent.ID)
	}
	if c.Canary != parent.Canary {
		add(c, "canary differs from parent %s", parent.ID)
	}
	if len(c.InjectedParagraphs) != 0 {
		add(c, "control injected_paragraphs must be empty")
	}
	if len(c.Documents) != len(parent.Documents) {
		add(c, "document count differs from parent %s", parent.ID)
		return
	}
	for i, d := range c.Documents {
		pd := parent.Documents[i]
		want := pd.Text
		if pd.ID == parent.InjectedDoc {
			want = StripParagraphs(pd.Text, parent.InjectedParagraphs)
		}
		if d.ID != pd.ID {
			add(c, "document %d id %q differs from parent's %q", i, d.ID, pd.ID)
		} else if d.Text != want {
			add(c, "document %s text is not the parent's text with injected paragraphs removed", d.ID)
		}
		if strings.Contains(d.Text, parent.Canary) {
			add(c, "control document %s contains the canary", d.ID)
		}
	}
}

// StripParagraphs removes the 1-based paragraphs from text and rejoins the rest with "\n\n".
func StripParagraphs(text string, drop []int) string {
	skip := map[int]bool{}
	for _, p := range drop {
		skip[p] = true
	}
	var keep []string
	for i, p := range Paragraphs(text) {
		if !skip[i+1] {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n\n")
}

func allQuoted(p string) bool {
	for _, l := range strings.Split(p, "\n") {
		if !strings.HasPrefix(l, "> ") {
			return false
		}
	}
	return true
}
