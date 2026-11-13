package intake

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// Ported from github.com/C360Studio/semsource/handler/doc/splitter_test.go,
// asciidoc_test.go, homogeneous_test.go, and isolation_test.go
// (commit 4ca2072c5d9d6bf4ec5ad76dc33b92d61c1614c1, MIT License).

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_TilesInputExactly(t *testing.T) {
	for name, body := range splitterFixtures() {
		t.Run(name, func(t *testing.T) {
			assertTiles(t, []byte(body))
			ps := Split("doc-1", body)
			for i, p := range ps {
				if p.DocID != "doc-1" || p.ID != PassageID("doc-1", i) {
					t.Fatalf("passage %d ID=%q DocID=%q, want %q / doc-1", i, p.ID, p.DocID, PassageID("doc-1", i))
				}
			}
		})
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_TilesRealCorpus(t *testing.T) {
	root := repoRoot(t)
	var checked int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == ".worktrees" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		t.Run(rel, func(t *testing.T) { assertTiles(t, content) })
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if checked < 10 {
		t.Fatalf("expected the repo markdown files to cover at least 10 documents, only checked %d", checked)
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_NeverExceedsHardMax(t *testing.T) {
	root := repoRoot(t)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if err == nil && info.IsDir() && (info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == ".worktrees") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, p := range splitPassages(content, formatMarkdown) {
			if size := p.End - p.Start; size > defaultBounds.HardMax {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s passage %d is %d bytes, above the hard max %d — it would be silently truncated",
					rel, p.Ordinal, size, defaultBounds.HardMax)
			}
		}
		return nil
	})
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_IsDeterministic(t *testing.T) {
	for name, body := range splitterFixtures() {
		t.Run(name, func(t *testing.T) {
			first := Split("doc-det", body)
			for i := 0; i < 5; i++ {
				again := Split("doc-det", body)
				if !reflect.DeepEqual(again, first) {
					t.Fatalf("run %d differs:\n got %+v\nwant %+v", i, again, first)
				}
			}
		})
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_SplitsOnHeadings(t *testing.T) {
	got := splitPassages([]byte(strings.Repeat("Preamble prose. ", 40)+"\n\n"+
		"# Alpha\n\n"+strings.Repeat("Alpha body. ", 40)+"\n\n"+
		"## Beta\n\n"+strings.Repeat("Beta body. ", 40)+"\n"), formatMarkdown)

	if len(got) != 3 {
		t.Fatalf("expected 3 passages (preamble, Alpha, Beta), got %d", len(got))
	}
	if got[0].Heading != "" {
		t.Errorf("preamble heading = %q, want empty", got[0].Heading)
	}
	if got[1].Heading != "Alpha" {
		t.Errorf("passage 1 heading = %q, want Alpha", got[1].Heading)
	}
	if got[2].Heading != "Beta" {
		t.Errorf("passage 2 heading = %q, want Beta", got[2].Heading)
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_HeadingPathSetextLevels(t *testing.T) {
	body := strings.Repeat("Filler prose to keep every section above the merge floor. ", 10)
	got := splitPassages([]byte("Alpha\n=====\n\n"+body+"\n"+
		"Beta\n----\n\n"+body+"\n"), formatMarkdown)

	want := map[string][]string{
		"Alpha": {"Alpha"},
		"Beta":  {"Alpha", "Beta"},
	}
	for _, p := range got {
		wantPath, ok := want[p.Heading]
		if !ok {
			t.Fatalf("unexpected heading %q", p.Heading)
		}
		if !reflect.DeepEqual(p.HeadingPath, wantPath) {
			t.Errorf("heading %q has path %v, want %v", p.Heading, p.HeadingPath, wantPath)
		}
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_MergedSectionsKeepFirstPath(t *testing.T) {
	got := splitPassages([]byte("## Outer\n\nshort\n\n### Absorbed\n\nalso short\n"), formatMarkdown)

	if len(got) != 1 {
		t.Fatalf("expected the two small sections to merge into 1 passage, got %d", len(got))
	}
	if !reflect.DeepEqual(got[0].HeadingPath, []string{"Outer"}) {
		t.Errorf("merged passage path = %v, want [Outer]", got[0].HeadingPath)
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestPassageTitle_AncestryAndCollapse(t *testing.T) {
	cases := []struct {
		name, parent string
		path         []string
		want         string
	}{
		{"ancestry spelled out", "Report A", []string{"Setup", "Configuration"}, "Report A § Setup § Configuration"},
		{"leading title dropped", "Report A", []string{"Report A", "Setup", "Configuration"}, "Report A § Setup § Configuration"},
		{"all collapsed", "README.md", []string{"README.md"}, "README.md"},
		{"no heading", "README", nil, "README § passage 1"},
	}
	for _, c := range cases {
		if got := PassageTitle(c.parent, c.path, 0); got != c.want {
			t.Errorf("%s: PassageTitle(%q, %v) = %q, want %q", c.name, c.parent, c.path, got, c.want)
		}
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_KeepsFencedCodeWhole(t *testing.T) {
	fence := "```go\n" + strings.Repeat("line of code\n\nblank separated\n", 60) + "```\n"
	content := []byte("# Heading\n\n" + strings.Repeat("prose. ", 100) + "\n\n" + fence)

	for _, p := range splitPassages(content, formatMarkdown) {
		opens := strings.Count(p.Body, "```")
		if opens%2 != 0 {
			t.Fatalf("passage %d splits a fenced block (odd number of fence markers):\n%s", p.Ordinal, p.Body)
		}
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_MergesTrivialSections(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		b.WriteString("## Tiny\n\nshort.\n\n")
	}
	got := splitPassages([]byte(b.String()), formatMarkdown)
	if len(got) >= 8 {
		t.Errorf("expected trivial sections to merge, got %d passages for 8 tiny sections", len(got))
	}
	if len(got) == 0 {
		t.Fatal("expected at least one passage")
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_SubdividesOversizedSection(t *testing.T) {
	body := "# Big\n\n" + strings.Repeat("This is a sentence of prose. ", 400)
	got := splitPassages([]byte(body), formatMarkdown)
	if len(got) < 2 {
		t.Fatalf("expected an oversized section to subdivide, got %d passages", len(got))
	}
	for _, p := range got {
		if p.Heading != "Big" {
			t.Errorf("subdivided passage %d lost its heading: %q", p.Ordinal, p.Heading)
		}
		if size := p.End - p.Start; size > defaultBounds.HardMax {
			t.Errorf("passage %d is %d bytes, above hard max", p.Ordinal, size)
		}
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_FrontMatterIsNotASetextHeading(t *testing.T) {
	content := []byte("---\ntitle: Something\nauthor: Someone\n---\n\n# Real Heading\n\nbody\n")
	got := splitPassages(content, formatMarkdown)
	for _, p := range got {
		if strings.Contains(p.Heading, "title:") || strings.Contains(p.Heading, "author:") {
			t.Errorf("frontmatter line became a heading: %q", p.Heading)
		}
	}
	if len(got) == 0 {
		t.Fatal("expected at least one passage")
	}
	assertTiles(t, content)
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_SetextHeadings(t *testing.T) {
	content := []byte("Title Here\n==========\n\n" + strings.Repeat("body. ", 100) +
		"\n\nSubtitle\n--------\n\n" + strings.Repeat("more. ", 100) + "\n")
	got := splitPassages(content, formatMarkdown)

	var headings []string
	for _, p := range got {
		headings = append(headings, p.Heading)
	}
	if !contains(headings, "Title Here") {
		t.Errorf("setext H1 not detected; headings = %v", headings)
	}
	if !contains(headings, "Subtitle") {
		t.Errorf("setext H2 not detected; headings = %v", headings)
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_HeadingInFenceIsNotAHeading(t *testing.T) {
	content := []byte("# Real\n\n```sh\n# not a heading\necho hi\n```\n\nbody\n")
	for _, p := range splitPassages(content, formatMarkdown) {
		if p.Heading == "not a heading" {
			t.Fatal("a comment inside a fenced block was treated as a heading")
		}
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_EmptyInput(t *testing.T) {
	if got := splitPassages(nil, formatMarkdown); got != nil {
		t.Errorf("expected nil for empty content, got %d passages", len(got))
	}
	if got := splitPassages([]byte(""), formatMarkdown); got != nil {
		t.Errorf("expected nil for empty content, got %d passages", len(got))
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_ShortDocumentIsOnePassage(t *testing.T) {
	got := splitPassages([]byte("# Small\n\nJust a little prose.\n"), formatMarkdown)
	if len(got) != 1 {
		t.Fatalf("expected one passage for a short document, got %d", len(got))
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestSplitPassages_MultiByteRunesAreNotCut(t *testing.T) {
	content := []byte("# Unicode\n\n" + strings.Repeat("日本語テキスト", 2000))
	got := splitPassages(content, formatMarkdown)
	if len(got) < 2 {
		t.Fatalf("expected the hard cut to engage, got %d passages", len(got))
	}
	for _, p := range got {
		if !utf8ValidString(p.Body) {
			t.Errorf("passage %d ends mid-rune", p.Ordinal)
		}
	}
	assertTiles(t, content)
}

//aip:verifies split-tiles-input-with-stable-ids
func TestEquivalentDocumentsAgreeAcrossFormats(t *testing.T) {
	pad := strings.Repeat("This clause defines normative requirements for the resource and its encodings. ", 40)
	bodyOf := func(h1, h2, h3 string) []byte {
		var b strings.Builder
		b.WriteString(h1 + " Connected Systems API\n\n" + pad + "\n\n")
		b.WriteString(h2 + " Feature Resources\n\n" + pad + "\n\n")
		b.WriteString(h3 + " Datastreams\n\n" + pad + "\n\n")
		b.WriteString(h2 + " Dynamic Data\n\n" + pad + "\n")
		return []byte(b.String())
	}
	md := splitPassages(bodyOf("#", "##", "###"), formatMarkdown)
	ad := splitPassages(bodyOf("=", "==", "==="), formatASCIIDoc)
	if len(md) != len(ad) {
		t.Fatalf("passage counts differ: markdown=%d asciidoc=%d", len(md), len(ad))
	}
	for i := range md {
		if md[i].Heading != ad[i].Heading {
			t.Errorf("passage %d heading: markdown=%q asciidoc=%q", i, md[i].Heading, ad[i].Heading)
		}
		if strings.Join(md[i].HeadingPath, " > ") != strings.Join(ad[i].HeadingPath, " > ") {
			t.Errorf("passage %d path: markdown=%v asciidoc=%v", i, md[i].HeadingPath, ad[i].HeadingPath)
		}
	}
}

//aip:verifies split-tiles-input-with-stable-ids
func TestKeyGroupsAndIsolateFencedBlocks(t *testing.T) {
	content := []byte("## Configuration\n\nSet these:\n\n```bash\n" +
		"ALPHA_ONE=1\n" +
		"BETA_TWO=2\n" +
		"GAMMA_URL=http://localhost:4222\n" +
		"GAMMA_PORT=4222\n" +
		"GAMMA_MONITOR_PORT=8222\n" +
		"```\n")
	ps := splitPassages(content, formatMarkdown)
	if len(ps) < 3 {
		t.Fatalf("expected key groups to divide block into >= 3 passages, got %d", len(ps))
	}
	assertTiles(t, content)

	prose := strings.Repeat("Explanatory prose that dilutes the block beside it. ", 12)
	isolatedContent := []byte("## Tier 2\n\n" + prose + "\n\n```bash\ndocker run -d -p 8083:8083 img\n```\n")
	assertTiles(t, isolatedContent)
}

// FuzzSplit checks the tiling, hard-max, UTF-8 rune-boundary, and determinism
// laws across generated documents.
//
//aip:verifies split-tiles-input-with-stable-ids
func FuzzSplit(f *testing.F) {
	for _, body := range splitterFixtures() {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) {
			return
		}
		ps := Split("doc", text)
		if text == "" {
			if len(ps) != 0 {
				t.Fatalf("non-empty passages for empty text: %+v", ps)
			}
			return
		}
		assertTiles(t, []byte(text))
		for i, p := range ps {
			if p.Ordinal != i || p.ID != PassageID("doc", i) {
				t.Fatalf("passage %d ID=%q Ordinal=%d", i, p.ID, p.Ordinal)
			}
			if p.End-p.Start > DefaultPassageBounds.HardMax {
				t.Fatalf("passage %d size %d exceeds HardMax %d", i, p.End-p.Start, DefaultPassageBounds.HardMax)
			}
			if !utf8.ValidString(p.Body) {
				t.Fatalf("passage %d split mid-rune", i)
			}
		}
	})
}

func assertTiles(t *testing.T, content []byte) {
	t.Helper()
	got := splitPassages(content, formatMarkdown)
	if len(content) == 0 {
		return
	}
	if len(got) == 0 {
		t.Fatal("non-empty content produced no passages")
	}

	var b strings.Builder
	prevEnd := 0
	for _, p := range got {
		if p.Start != prevEnd {
			t.Fatalf("passage %d starts at %d, previous ended at %d — %s",
				p.Ordinal, p.Start, prevEnd,
				map[bool]string{true: "gap", false: "overlap"}[p.Start > prevEnd])
		}
		if p.Body != string(content[p.Start:p.End]) {
			t.Fatalf("passage %d body does not match its byte span", p.Ordinal)
		}
		b.WriteString(p.Body)
		prevEnd = p.End
	}
	if prevEnd != len(content) {
		t.Fatalf("passages end at %d, content is %d bytes — the tail is unreachable", prevEnd, len(content))
	}
	if b.String() != string(content) {
		t.Fatal("concatenated passages do not reproduce the input")
	}
}

func splitterFixtures() map[string]string {
	return map[string]string{
		"empty-ish":       "\n",
		"no-headings":     strings.Repeat("Just prose with no structure at all. ", 200),
		"single-heading":  "# Only\n\nbody\n",
		"preamble":        "intro\n\n# One\n\nbody\n",
		"trailing-blanks": "# One\n\nbody\n\n\n\n",
		"no-trailing-nl":  "# One\n\nbody without a trailing newline",
		"crlf":            "# One\r\n\r\nbody\r\n",
		"frontmatter":     "---\ntitle: X\n---\n\n# One\n\nbody\n",
		"fence":           "# One\n\n```go\nfunc main() {}\n```\n\nafter\n",
		"nested-fence":    "# One\n\n````md\n```go\ncode\n```\n````\n\nafter\n",
		"consecutive-h":   "# A\n## B\n### C\n\nbody\n",
		"huge-paragraph":  "# Big\n\n" + strings.Repeat("word ", 3000),
		"huge-fence":      "# Big\n\n```\n" + strings.Repeat("code line\n", 1500) + "```\n",
		"unicode":         "# 日本語\n\n" + strings.Repeat("テキストの段落。", 500),
		"tabs-and-spaces": "#\tTabbed\n\n\tindented block\n\nbody\n",
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == utf8.RuneError {
			return false
		}
	}
	return true
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate repository root")
	return ""
}
