// Package intake cleans untrusted research provider responses, parses Exa
// search and contents wire payloads, splits documents into structural passages
// with stable IDs and byte offsets, builds an in-memory provenance graph, and
// runs deterministic passage checks and band scoring.
package intake

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Ported from github.com/C360Studio/semsource/handler/doc/splitter.go
// (commit 4ca2072c5d9d6bf4ec5ad76dc33b92d61c1614c1, MIT License, Copyright (c) 2026 Complete 360 Studio).
// See THIRD_PARTY_NOTICES.md.

// PassageBounds is the set of passage size bounds, in bytes, that govern how
// sections are merged and split into passages.
type PassageBounds struct {
	Ceiling int
	Floor   int
	HardMax int
}

type passageBounds = PassageBounds

// DefaultPassageBounds are the shipped passage size bounds (2000 / 400 / 6000 bytes).
var DefaultPassageBounds = PassageBounds{Ceiling: 2000, Floor: 400, HardMax: 6000}

var defaultBounds = DefaultPassageBounds

// Passage is one retrievable slice of a document. Start and End are byte
// offsets into the input content, and Body is content[Start:End] — passages
// tile the document with no gap and no overlap, so concatenating every Body in
// ordinal order reproduces the input byte for byte.
type Passage struct {
	ID          string   `json:"id"`
	DocID       string   `json:"doc_id,omitempty"`
	Ordinal     int      `json:"ordinal"`
	Heading     string   `json:"heading,omitempty"`
	HeadingPath []string `json:"heading_path,omitempty"`
	Start       int      `json:"start"`
	End         int      `json:"end"`
	Body        string   `json:"body"`
}

type passage = Passage

// PassageID returns a deterministic passage identifier derived from docID and
// 0-based ordinal (`<docID>#p0000`).
func PassageID(docID string, ordinal int) string {
	if docID == "" {
		return fmt.Sprintf("p%04d", ordinal)
	}
	return fmt.Sprintf("%s#p%04d", docID, ordinal)
}

// PassageTitle qualifies a passage's title with its parent's and the passage's
// full heading ancestry, dropping leading components that repeat parentTitle.
func PassageTitle(parentTitle string, path []string, ordinal int) string {
	return passageTitle(parentTitle, path, ordinal)
}

func passageTitle(parentTitle string, path []string, ordinal int) string {
	trimmed := path
	for len(trimmed) > 0 && strings.EqualFold(strings.TrimSpace(trimmed[0]), strings.TrimSpace(parentTitle)) {
		trimmed = trimmed[1:]
	}
	if len(trimmed) > 0 {
		return parentTitle + " § " + strings.Join(trimmed, " § ")
	}
	if len(path) > 0 {
		return parentTitle
	}
	return fmt.Sprintf("%s § passage %d", parentTitle, ordinal+1)
}

// Split divides text into structural passages with deterministic IDs derived
// from docID and 0-based ordinal, using DefaultPassageBounds and Markdown heading syntax.
func Split(docID, text string) []Passage {
	ps := splitPassages([]byte(text), formatMarkdown)
	for i := range ps {
		ps[i].DocID = docID
		ps[i].ID = PassageID(docID, ps[i].Ordinal)
	}
	return ps
}

// SplitBounded divides text into structural passages using explicit PassageBounds.
func SplitBounded(docID, text string, b PassageBounds) []Passage {
	ps := splitPassagesBounded([]byte(text), b, formatMarkdown)
	for i := range ps {
		ps[i].DocID = docID
		ps[i].ID = PassageID(docID, ps[i].Ordinal)
	}
	return ps
}

// line is one physical line of the document, carrying its byte span and whether
// it sits inside a fenced code block or YAML frontmatter (in either case it is
// not eligible to be a heading).
type line struct {
	start, end int
	text       string
	protected  bool
}

// splitPassages divides content into passages on structural boundaries, using
// the shipped default bounds. It is a pure function of the input bytes AND the
// document format: the same content in the same format always yields the same
// boundaries, independent of machine, run order, or wall clock.
func splitPassages(content []byte, f docFormat) []passage {
	return splitPassagesBounded(content, defaultBounds, f)
}

// splitPassagesBounded is splitPassages parameterized on passageBounds, so a
// caller can vary the size bounds without touching the shipped defaults.
func splitPassagesBounded(content []byte, b passageBounds, f docFormat) []passage {
	if len(content) == 0 {
		return nil
	}

	lines := scanLines(content)
	sections := sectionsOf(lines, len(content), f)
	sections = mergeSmallSections(sections, b)

	var out []passage
	for _, s := range sections {
		for _, span := range subdivide(content, lines, s, b) {
			ord := len(out)
			out = append(out, passage{
				ID:          PassageID("", ord),
				Ordinal:     ord,
				Heading:     s.heading,
				HeadingPath: s.path,
				Start:       span[0],
				End:         span[1],
				Body:        string(content[span[0]:span[1]]),
			})
		}
	}
	return out
}

// scanLines splits content into lines and marks the ones that cannot host a
// heading: everything inside a fenced code block (including the fence
// delimiters) and everything inside leading YAML frontmatter.
func scanLines(content []byte) []line {
	var lines []line
	for off := 0; off < len(content); {
		end := off
		for end < len(content) && content[end] != '\n' {
			end++
		}
		if end < len(content) {
			end++ // include the newline
		}
		lines = append(lines, line{start: off, end: end, text: strings.TrimRight(string(content[off:end]), "\r\n")})
		off = end
	}

	inFence := false
	inFrontMatter := len(lines) > 0 && strings.TrimSpace(lines[0].text) == "---"
	for i := range lines {
		trimmed := strings.TrimSpace(lines[i].text)
		if inFrontMatter {
			lines[i].protected = true
			if i > 0 && trimmed == "---" {
				inFrontMatter = false
			}
			continue
		}
		if isFenceDelimiter(trimmed) {
			inFence = !inFence
			lines[i].protected = true
			continue
		}
		lines[i].protected = inFence
	}
	return lines
}

func isFenceDelimiter(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// section is a heading and the byte span it governs.
type section struct {
	heading    string
	path       []string
	start, end int
}

// sectionsOf splits the document at heading boundaries.
func sectionsOf(lines []line, total int, f docFormat) []section {
	type mark struct {
		lineIdx int
		level   int
		heading string
	}
	var marks []mark
	for i := range lines {
		if text, level, ok := headingAt(lines, i, f); ok {
			marks = append(marks, mark{lineIdx: i, level: level, heading: text})
		}
	}

	if len(marks) == 0 {
		return []section{{start: 0, end: total}}
	}

	var ancestors []mark
	pathOf := func(m mark) []string {
		for len(ancestors) > 0 && ancestors[len(ancestors)-1].level >= m.level {
			ancestors = ancestors[:len(ancestors)-1]
		}
		path := make([]string, 0, len(ancestors)+1)
		for _, a := range ancestors {
			path = append(path, a.heading)
		}
		path = append(path, m.heading)
		ancestors = append(ancestors, m)
		return path
	}

	var out []section
	if marks[0].lineIdx > 0 {
		out = append(out, section{start: 0, end: lines[marks[0].lineIdx].start})
	}
	for i, m := range marks {
		end := total
		if i+1 < len(marks) {
			end = lines[marks[i+1].lineIdx].start
		}
		out = append(out, section{heading: m.heading, path: pathOf(m), start: lines[m.lineIdx].start, end: end})
	}
	return out
}

type docFormat int

const (
	formatMarkdown docFormat = iota
	formatASCIIDoc
)

func headingAt(lines []line, i int, f docFormat) (string, int, bool) {
	if f == formatASCIIDoc {
		return adocHeading(lines, i)
	}
	if text, level, ok := atxHeading(lines, i); ok {
		return text, level, true
	}
	return setextHeading(lines, i)
}

func adocHeading(lines []line, i int) (string, int, bool) {
	if lines[i].protected {
		return "", 0, false
	}
	t := strings.TrimSpace(lines[i].text)
	equals := 0
	for equals < len(t) && t[equals] == '=' {
		equals++
	}
	if equals == 0 || equals > 6 {
		return "", 0, false
	}
	rest := t[equals:]
	if !strings.HasPrefix(rest, " ") {
		return "", 0, false
	}
	return strings.TrimSpace(rest), equals, true
}

func atxHeading(lines []line, i int) (string, int, bool) {
	if lines[i].protected {
		return "", 0, false
	}
	t := strings.TrimSpace(lines[i].text)
	hashes := 0
	for hashes < len(t) && t[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return "", 0, false
	}
	rest := t[hashes:]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", 0, false
	}
	return strings.TrimSpace(strings.TrimRight(rest, "#")), hashes, true
}

func setextHeading(lines []line, i int) (string, int, bool) {
	if lines[i].protected || i+1 >= len(lines) || lines[i+1].protected {
		return "", 0, false
	}
	text := strings.TrimSpace(lines[i].text)
	if text == "" || strings.HasPrefix(text, "#") {
		return "", 0, false
	}
	switch text[0] {
	case '-', '*', '+', '>', '|', '=':
		return "", 0, false
	}
	under := strings.TrimSpace(lines[i+1].text)
	if len(under) < 2 {
		return "", 0, false
	}
	c := under[0]
	if c != '=' && c != '-' {
		return "", 0, false
	}
	if strings.Trim(under, string(c)) != "" {
		return "", 0, false
	}
	level := 1
	if c == '-' {
		level = 2
	}
	return text, level, true
}

func mergeSmallSections(in []section, b passageBounds) []section {
	if len(in) == 0 {
		return in
	}
	out := []section{in[0]}
	for _, s := range in[1:] {
		prev := &out[len(out)-1]
		prevSize := prev.end - prev.start
		if prevSize < b.Floor && (s.end-prev.start) <= b.Ceiling {
			prev.end = s.end
			continue
		}
		out = append(out, s)
	}
	return out
}

func subdivide(content []byte, lines []line, s section, b passageBounds) [][2]int {
	if spans := splitHomogeneousBlocks(lines, s); spans != nil {
		return spans
	}
	if spans := isolateFencedBlocks(lines, s, b); spans != nil {
		return spans
	}
	if s.end-s.start <= b.Ceiling {
		return [][2]int{{s.start, s.end}}
	}

	var out [][2]int
	cur := [2]int{s.start, s.start}
	flush := func() {
		if cur[1] > cur[0] {
			out = append(out, cur)
		}
	}
	for _, blk := range blocksOf(lines, s) {
		size := blk[1] - blk[0]
		if cur[1] > cur[0] && (cur[1]-cur[0])+size > b.Ceiling {
			flush()
			cur = [2]int{blk[0], blk[0]}
		}
		if size > b.Ceiling {
			flush()
			out = append(out, splitOversized(content, lines, blk, b)...)
			cur = [2]int{blk[1], blk[1]}
			continue
		}
		cur[1] = blk[1]
	}
	flush()
	if len(out) == 0 {
		return [][2]int{{s.start, s.end}}
	}
	return out
}

func blocksOf(lines []line, s section) [][2]int {
	var out [][2]int
	cur := [2]int{-1, -1}
	for i := range lines {
		ln := lines[i]
		if ln.start < s.start || ln.end > s.end {
			continue
		}
		blank := strings.TrimSpace(ln.text) == "" && !ln.protected
		if blank {
			if cur[0] >= 0 {
				cur[1] = ln.end
				out = append(out, cur)
				cur = [2]int{-1, -1}
			} else if len(out) > 0 {
				out[len(out)-1][1] = ln.end
			}
			continue
		}
		if cur[0] < 0 {
			cur = [2]int{ln.start, ln.end}
		} else {
			cur[1] = ln.end
		}
	}
	if cur[0] >= 0 {
		out = append(out, cur)
	}
	if len(out) == 0 {
		return [][2]int{{s.start, s.end}}
	}
	out[0][0] = s.start
	out[len(out)-1][1] = s.end
	return out
}

func splitOversized(content []byte, lines []line, blk [2]int, b passageBounds) [][2]int {
	if blockIsFenced(lines, blk) {
		if blk[1]-blk[0] <= b.HardMax {
			return [][2]int{blk}
		}
		return cutOnLines(lines, blk, b)
	}
	return cutOnSentences(content, blk, b)
}

func blockIsFenced(lines []line, blk [2]int) bool {
	for i := range lines {
		if lines[i].start >= blk[0] && lines[i].end <= blk[1] && lines[i].protected {
			return true
		}
	}
	return false
}

func cutOnLines(lines []line, blk [2]int, b passageBounds) [][2]int {
	var out [][2]int
	cur := [2]int{blk[0], blk[0]}
	for i := range lines {
		ln := lines[i]
		if ln.start < blk[0] || ln.end > blk[1] {
			continue
		}
		if cur[1] > cur[0] && (ln.end-cur[0]) > b.HardMax {
			out = append(out, cur)
			cur = [2]int{ln.start, ln.end}
			continue
		}
		cur[1] = ln.end
	}
	if cur[1] > cur[0] {
		out = append(out, cur)
	}
	return out
}

func cutOnSentences(content []byte, blk [2]int, b passageBounds) [][2]int {
	var out [][2]int
	start := blk[0]
	last := blk[0]
	for _, end := range sentenceEnds(content, blk) {
		if end-start > b.Ceiling && last > start {
			out = append(out, [2]int{start, last})
			start = last
		}
		for end-start > b.HardMax {
			cut := runeBoundary(content, start+b.HardMax)
			out = append(out, [2]int{start, cut})
			start = cut
		}
		last = end
	}
	if blk[1] > start {
		for blk[1]-start > b.HardMax {
			cut := runeBoundary(content, start+b.HardMax)
			out = append(out, [2]int{start, cut})
			start = cut
		}
		out = append(out, [2]int{start, blk[1]})
	}
	return out
}

func sentenceEnds(content []byte, blk [2]int) []int {
	var ends []int
	for i := blk[0]; i < blk[1]; i++ {
		switch content[i] {
		case '.', '!', '?':
			j := i + 1
			if j < blk[1] && (content[j] == '"' || content[j] == '\'' || content[j] == ')') {
				j++
			}
			if j >= blk[1] || unicode.IsSpace(rune(content[j])) {
				ends = append(ends, j)
			}
		}
	}
	return ends
}

func runeBoundary(content []byte, i int) int {
	if i >= len(content) {
		return len(content)
	}
	for i > 0 && !utf8.RuneStart(content[i]) {
		i--
	}
	return i
}

const minKeyGroups = 3
const minKeyValueShare = 0.6

func splitHomogeneousBlocks(lines []line, s section) [][2]int {
	var out [][2]int
	cursor := s.start
	found := false
	for _, blk := range blocksOf(lines, s) {
		groups := keyValueGroups(lines, blk)
		if groups == nil {
			continue
		}
		found = true
		if groups[0][0] > cursor {
			out = append(out, [2]int{cursor, groups[0][0]})
		}
		out = append(out, groups...)
		cursor = groups[len(groups)-1][1]
	}
	if !found {
		return nil
	}
	if cursor < s.end {
		out = append(out, [2]int{cursor, s.end})
	}
	return out
}

func isolateFencedBlocks(lines []line, s section, b passageBounds) [][2]int {
	var fenced [][2]int
	for _, blk := range blocksOf(lines, s) {
		if blockIsFenced(lines, blk) {
			fenced = append(fenced, blk)
		}
	}
	if len(fenced) == 0 {
		return nil
	}

	var fencedBytes int
	for _, f := range fenced {
		fencedBytes += f[1] - f[0]
	}
	if (s.end-s.start)-fencedBytes < b.Floor {
		return nil
	}

	var out [][2]int
	cursor := s.start
	for _, f := range fenced {
		if f[0] > cursor {
			out = append(out, [2]int{cursor, f[0]})
		}
		out = append(out, f)
		cursor = f[1]
	}
	if cursor < s.end {
		out = append(out, [2]int{cursor, s.end})
	}
	return out
}

func keyValueGroups(lines []line, blk [2]int) [][2]int {
	if !blockIsFenced(lines, blk) {
		return nil
	}
	var content, assignments int
	var groups [][2]int
	prefix := ""
	for i := range lines {
		ln := lines[i]
		if ln.start < blk[0] || ln.end > blk[1] {
			continue
		}
		trimmed := strings.TrimSpace(ln.text)
		if trimmed == "" || isFenceDelimiter(trimmed) {
			continue
		}
		content++
		key, ok := assignmentPrefix(trimmed)
		if !ok {
			prefix = ""
			continue
		}
		assignments++
		if key == prefix && len(groups) > 0 {
			groups[len(groups)-1][1] = ln.end
			continue
		}
		groups = append(groups, [2]int{ln.start, ln.end})
		prefix = key
	}
	if content == 0 || len(groups) < minKeyGroups {
		return nil
	}
	if float64(assignments)/float64(content) < minKeyValueShare {
		return nil
	}
	return groups
}

func assignmentPrefix(trimmed string) (string, bool) {
	eq := strings.IndexByte(trimmed, '=')
	if eq <= 0 {
		return "", false
	}
	key := trimmed[:eq]
	for i, r := range key {
		valid := r == '_' || unicode.IsDigit(r) || unicode.IsLetter(r)
		if !valid || (i == 0 && unicode.IsDigit(r)) {
			return "", false
		}
	}
	if under := strings.IndexByte(key, '_'); under > 0 {
		return key[:under], true
	}
	return key, true
}
