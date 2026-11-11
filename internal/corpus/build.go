package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DraftCase is one authored case in corpus/draft/<class>.json.
type DraftCase struct {
	N                  int        `json:"n"`
	Question           string     `json:"question"`
	Documents          []Document `json:"documents"`
	InjectedDoc        string     `json:"injected_doc"`
	InjectedParagraphs []int      `json:"injected_paragraphs"`
}

// Draft is one authored class file.
type Draft struct {
	Class string      `json:"class"`
	Cases []DraftCase `json:"cases"`
}

// Assemble turns drafts into ordered cases with controls. existing maps case id to
// a frozen canary; ids without one draw from newCanary, which must not repeat a used value.
func Assemble(drafts map[string]Draft, existing map[string]string, newCanary func() (string, error)) ([]Case, error) {
	used := map[string]bool{}
	for _, c := range existing {
		used[c] = true
	}
	var out []Case
	for _, class := range Classes {
		d, ok := drafts[class]
		if !ok {
			continue
		}
		for _, dc := range d.Cases {
			id := fmt.Sprintf("%s-%03d", class, dc.N)
			canary, frozen := existing[id]
			if !frozen {
				for {
					c, err := newCanary()
					if err != nil {
						return nil, fmt.Errorf("canary for %s: %w", id, err)
					}
					if !used[c] {
						canary = c
						used[c] = true
						break
					}
				}
			}
			docs := make([]Document, len(dc.Documents))
			for i, doc := range dc.Documents {
				docs[i] = Document{ID: doc.ID, Text: strings.ReplaceAll(doc.Text, "{{CANARY}}", canary)}
			}
			c := Case{
				ID: id, Class: class, Task: "score-compare", Question: dc.Question, Documents: docs,
				InjectedDoc: dc.InjectedDoc, InjectedParagraphs: dc.InjectedParagraphs,
				Canary: canary, Ask: asks[class],
			}
			if c.InjectedParagraphs == nil {
				c.InjectedParagraphs = []int{}
			}
			out = append(out, c)
			if isAttack(class) {
				out = append(out, deriveControl(c))
			}
		}
	}
	return out, nil
}

func deriveControl(p Case) Case {
	docs := make([]Document, len(p.Documents))
	for i, d := range p.Documents {
		docs[i] = d
		if d.ID == p.InjectedDoc {
			docs[i].Text = StripParagraphs(d.Text, p.InjectedParagraphs)
		}
	}
	parent := p.ID
	c := p
	c.ID = p.ID + "-control"
	c.Documents = docs
	c.InjectedParagraphs = []int{}
	c.ControlOf = &parent
	return c
}

// BuildResult reports what Build wrote.
type BuildResult struct {
	Cases    int
	SHA256   string
	Warnings []string
}

// Build reads dir/draft/*.json, writes dir/cases.json and dir/corpus.lock.json.
// Canaries already in dir/cases.json are reused by id. Nothing is written if validation fails.
func Build(dir string, newCanary func() (string, error)) (BuildResult, error) {
	var res BuildResult
	tb, err := readBounded(filepath.Join(dir, "tasks.json"))
	if err != nil {
		return res, fmt.Errorf("read tasks.json: %w", err)
	}
	var tf tasksFile
	if err := decodeStrict(tb, &tf); err != nil {
		return res, fmt.Errorf("parse tasks.json: %w", err)
	}
	drafts := map[string]Draft{}
	for _, class := range Classes {
		b, err := readBounded(filepath.Join(dir, "draft", class+".json"))
		if errors.Is(err, os.ErrNotExist) {
			res.Warnings = append(res.Warnings, "draft missing for class "+class)
			continue
		}
		if err != nil {
			return res, fmt.Errorf("read draft %s: %w", class, err)
		}
		var d Draft
		if err := decodeStrict(b, &d); err != nil {
			return res, fmt.Errorf("parse draft %s: %w", class, err)
		}
		if d.Class != class {
			return res, fmt.Errorf("draft %s.json declares class %q", class, d.Class)
		}
		drafts[class] = d
	}
	existing := map[string]string{}
	if b, err := readBounded(filepath.Join(dir, "cases.json")); err == nil {
		var old casesFile
		if err := decodeStrict(b, &old); err != nil {
			return res, fmt.Errorf("parse existing cases.json: %w", err)
		}
		for _, c := range old.Cases {
			existing[c.ID] = c.Canary
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("read existing cases.json: %w", err)
	}
	cases, err := Assemble(drafts, existing, newCanary)
	if err != nil {
		return res, err
	}
	verr := Validate(cases, tf.Tasks)
	if len(drafts) == len(Classes) {
		verr = errors.Join(verr, CheckCounts(cases))
	}
	if verr != nil {
		return res, verr
	}
	cb, err := marshal(casesFile{Version: 1, Cases: cases})
	if err != nil {
		return res, err
	}
	if err := writeAtomic(filepath.Join(dir, "cases.json"), cb); err != nil {
		return res, err
	}
	lock := Lock{Version: 1}
	for _, f := range []struct {
		name string
		b    []byte
	}{{"cases.json", cb}, {"tasks.json", tb}} {
		sum := sha256.Sum256(f.b)
		lock.Files = append(lock.Files, LockEntry{Path: f.name, Size: int64(len(f.b)), SHA256: hex.EncodeToString(sum[:])})
	}
	lb, err := marshal(lock)
	if err != nil {
		return res, err
	}
	if err := writeAtomic(filepath.Join(dir, "corpus.lock.json"), lb); err != nil {
		return res, err
	}
	res.Cases = len(cases)
	res.SHA256 = lock.Files[0].SHA256
	return res, nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// FileSHA256 returns the hex SHA-256 of a bounded file.
func FileSHA256(path string) (string, error) {
	b, err := readBounded(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
