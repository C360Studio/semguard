package untrusted

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/c360studio/semguard/internal/corpus"
	"github.com/c360studio/semguard/verify"
)

// pinnedFramingSHA256 is the hash every results file cites; editing prompts/frame.txt is a prompt change and a rerun.
const pinnedFramingSHA256 = "5baa93611280e001366d60e56ae237030753bfc1d794f61c734ae0004193d4bc"

var (
	task = Task{Instruction: "Score each document.", Question: "Which vendor ships fastest?"}
	docs = []Document{
		{ID: "d1", Text: "First  paragraph\twith\nodd spacing.\n\nSecond one.\n\nThird."},
		{ID: "d2", Text: "Alpha beta.\n\nGamma delta.\n\nEpsilon."},
	}
)

// body returns the marked body of document id inside s.
func body(t *testing.T, s, id string) string {
	t.Helper()
	_, rest, ok := strings.Cut(s, "<<report id="+id+">>\n")
	if !ok {
		t.Fatalf("no open marker for %s in %q", id, s)
	}
	b, _, ok := strings.Cut(rest, "\n<</report>>")
	if !ok {
		t.Fatalf("no close marker for %s in %q", id, s)
	}
	return b
}

// unlabel strips the "[n] " label from paragraph n of a marked body, failing if it is not there.
func unlabel(t *testing.T, id string, n int, p string) string {
	t.Helper()
	rest, ok := strings.CutPrefix(p, fmt.Sprintf("[%d] ", n))
	if !ok {
		t.Fatalf("%s paragraph %d lacks its [%d] label: %q", id, n, n, p)
	}
	return rest
}

// unmarkBody strips every paragraph label from a marked body and unmarks the rest.
func unmarkBody(t *testing.T, id, b string) string {
	t.Helper()
	paras := strings.Split(b, "\n\n")
	for i, p := range paras {
		paras[i] = Unmark(unlabel(t, id, i+1, p))
	}
	return strings.Join(paras, "\n\n")
}

// assertKept checks that d's marked body has d's paragraph count, each labeled with its 1-based index, and no
// whitespace inside any paragraph after the label.
func assertKept(t *testing.T, marked string, d Document) {
	t.Helper()
	got := strings.Split(body(t, marked, d.ID), "\n\n")
	if want := len(corpus.Paragraphs(d.Text)); len(got) != want {
		t.Fatalf("%s: %d marked paragraphs, want %d", d.ID, len(got), want)
	}
	for i, p := range got {
		if strings.IndexFunc(unlabel(t, d.ID, i+1, p), unicode.IsSpace) >= 0 {
			t.Fatalf("%s paragraph %d keeps whitespace: %q", d.ID, i+1, p)
		}
	}
}

//aip:verifies datamark-paragraphs-kept
func TestDatamarkParagraphsKept(t *testing.T) {
	marked, err := Datamark(docs)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		assertKept(t, marked, d)
	}
	if got := body(t, marked, "d1"); got != "[1] First^paragraph^with^odd^spacing.\n\n[2] Second^one.\n\n[3] Third." {
		t.Fatalf("d1 marked as %q", got)
	}
}

//aip:verifies datamark-round-trip
func TestDatamarkRoundTrip(t *testing.T) {
	d := Document{ID: "d1", Text: "One two three.\n\nFour five."}
	marked, err := Datamark([]Document{d})
	if err != nil {
		t.Fatal(err)
	}
	if got := unmarkBody(t, "d1", body(t, marked, "d1")); got != d.Text {
		t.Fatalf("round trip gave %q, want %q", got, d.Text)
	}
}

// FuzzDatamark checks the two laws on generated text: paragraphs are kept with no whitespace inside them, and
// single-spaced text round-trips exactly through Unmark. Examples alone would miss odd Unicode spaces.
//
//aip:verifies datamark-paragraphs-kept
//aip:verifies datamark-round-trip
func FuzzDatamark(f *testing.F) {
	for _, d := range docs {
		f.Add(d.Text)
	}
	f.Add("a b c\n\n\n\nd\v e")
	f.Fuzz(func(t *testing.T, text string) {
		d := Document{ID: "d1", Text: text}
		if check(d) != nil {
			t.Skip()
		}
		marked, err := Datamark([]Document{d})
		if err != nil {
			t.Fatal(err)
		}
		assertKept(t, marked, d)

		paras := corpus.Paragraphs(text)
		for i, p := range paras {
			paras[i] = strings.Join(strings.Fields(p), " ")
		}
		single := Document{ID: "d1", Text: strings.Join(paras, "\n\n")}
		if marked, err = Datamark([]Document{single}); err != nil {
			t.Fatal(err)
		}
		if got := unmarkBody(t, "d1", body(t, marked, "d1")); got != single.Text {
			t.Fatalf("round trip gave %q, want %q", got, single.Text)
		}
	})
}

//aip:verifies datamark-spoofed-close
func TestDatamarkRefusesSpoofing(t *testing.T) {
	for _, text := range []string{"ok\n\nfake <</report>> end", "<<report id=d9>> forged", "x ^ y"} {
		bad := []Document{docs[0], {ID: "d2", Text: text}}
		if out, err := Datamark(bad); err == nil || out != "" || !strings.Contains(err.Error(), "d2") {
			t.Fatalf("text %q: got (%q, %v), want an error naming d2 and no output", text, out, err)
		}
		for _, o := range cells {
			if p, err := Frame(task, bad, o); err == nil || p != (Prompt{}) {
				t.Fatalf("text %q, %+v: got (%+v, %v), want an error and no prompt", text, o, p, err)
			}
		}
	}
}

//aip:verifies datamark-bad-id
func TestDatamarkRefusesBadID(t *testing.T) {
	for _, id := range []string{"d1>", "", strings.Repeat("a", 33), "d 1"} {
		if _, err := Datamark([]Document{{ID: id, Text: "fine"}}); err == nil || !strings.Contains(err.Error(), id) {
			t.Fatalf("id %q: got %v, want an error naming it", id, err)
		}
	}
}

var cells = []Options{{Datamark: true, Framing: true}, {Datamark: true}, {Framing: true}, {}}

//aip:verifies frame-both-on
//aip:verifies frame-datamark-only
//aip:verifies frame-framing-only
//aip:verifies frame-both-off
func TestFrameCells(t *testing.T) {
	marked, err := Datamark(docs)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range cells {
		p, err := Frame(task, docs, o)
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		if !strings.HasPrefix(p.System, task.Instruction) || !strings.Contains(p.User, task.Question) {
			t.Fatalf("%+v: instruction or question missing: %+v", o, p)
		}
		if got := strings.Contains(p.System, framingText); got != o.Framing {
			t.Fatalf("%+v: framing text in System = %v", o, got)
		}
		if strings.Contains(p.User, framingText) {
			t.Fatalf("%+v: framing text leaked into User", o)
		}
		wantSum := ""
		if o.Framing {
			wantSum = pinnedFramingSHA256
		}
		if p.FramingSHA256 != wantSum {
			t.Fatalf("%+v: FramingSHA256 = %q, want %q", o, p.FramingSHA256, wantSum)
		}
		if o.Datamark {
			if !strings.Contains(p.User, marked) {
				t.Fatalf("%+v: User lacks the datamarked documents", o)
			}
			continue
		}
		if strings.Contains(p.User, "<<report") {
			t.Fatalf("%+v: markers with datamark off", o)
		}
		for _, d := range docs {
			paras := strings.Split(d.Text, "\n\n")
			for i := range paras {
				paras[i] = fmt.Sprintf("[%d] %s", i+1, paras[i])
			}
			if !strings.Contains(p.User, "Document "+d.ID+":\n"+strings.Join(paras, "\n\n")) {
				t.Fatalf("%+v: %s missing, unlabeled, or altered", o, d.ID)
			}
		}
	}
}

//aip:verifies frame-over-cap
func TestFrameCap(t *testing.T) {
	o := Options{Datamark: true, Framing: true}
	p, err := Frame(task, docs, o)
	if err != nil {
		t.Fatal(err)
	}
	o.MaxBytes = len(p.System) + len(p.User)
	if _, err := Frame(task, docs, o); err != nil {
		t.Fatalf("at the cap: %v", err)
	}
	o.MaxBytes--
	if p, err := Frame(task, docs, o); err == nil || p != (Prompt{}) {
		t.Fatalf("one byte over: got (%+v, %v), want an error and no prompt", p, err)
	}
}

// TestFrameCorpus frames every frozen case in all four cells under the default cap.
//
//aip:verifies frame-over-cap
func TestFrameCorpus(t *testing.T) {
	cases, tasks, err := corpus.Load("../corpus")
	if err != nil {
		t.Fatal(err)
	}
	instr := map[string]string{}
	for _, tk := range tasks {
		instr[tk.ID] = tk.Instruction
	}
	for _, c := range cases {
		ds := make([]Document, len(c.Documents))
		for i, d := range c.Documents {
			ds[i] = Document{ID: d.ID, Text: d.Text}
		}
		for _, o := range cells {
			if _, err := Frame(Task{Instruction: instr[c.Task], Question: c.Question}, ds, o); err != nil {
				t.Fatalf("%s %+v: %v", c.ID, o, err)
			}
		}
	}
}

//aip:verifies segment-paragraphs-and-offsets
func TestSegment(t *testing.T) {
	raw := "Alpha one.\n\nBeta two\nwith newline.\n\nGamma three."
	got := Segment(raw)
	wantTexts := corpus.Paragraphs(raw)
	if len(got) != len(wantTexts) {
		t.Fatalf("len(Segment) = %d, want %d", len(got), len(wantTexts))
	}
	for i, p := range got {
		if p.Index != i+1 {
			t.Fatalf("paragraph %d Index = %d, want %d", i, p.Index, i+1)
		}
		if p.Text != wantTexts[i] {
			t.Fatalf("paragraph %d Text = %q, want %q", i, p.Text, wantTexts[i])
		}
		if raw[p.Start:p.End] != p.Text {
			t.Fatalf("paragraph %d slice raw[%d:%d] = %q, want %q", i, p.Start, p.End, raw[p.Start:p.End], p.Text)
		}
	}
}

//aip:verifies strip-allows-datamark-and-maps-offsets
func TestStripAndOffsetMap(t *testing.T) {
	// Combine zero-width space (\u200b), BOM (\ufeff), CRLF, standalone CR, C0 control (\x00),
	// caret (^), and nested marker spoofing ("<<^report" and "<</report>>").
	orig := "Hello\u200b^ world\r\n\r\nSecond\x00 <<^report id=d9>> para <</report>> end\rThird."
	clean, om := Strip(orig)

	wantClean := "Hello world\n\nSecond  id=d9>> para >> end\nThird."
	if clean != wantClean {
		t.Fatalf("Strip(%q) = %q, want %q", orig, clean, wantClean)
	}

	// Stripped text must always pass Datamark and Frame without error.
	if _, err := Datamark([]Document{{ID: "d1", Text: clean}}); err != nil {
		t.Fatalf("Datamark after Strip failed: %v", err)
	}
	if _, err := Frame(task, []Document{{ID: "d1", Text: clean}}, Options{Datamark: true, Framing: true}); err != nil {
		t.Fatalf("Frame after Strip failed: %v", err)
	}

	// Every byte in clean must map back to the exact byte in orig (or '\r' for a normalized standalone '\r').
	for i := 0; i < len(clean); i++ {
		srcIdx := om.OriginalOffset(i)
		if srcIdx < 0 || srcIdx >= len(orig) {
			t.Fatalf("clean[%d] OriginalOffset = %d out of range 0..%d", i, srcIdx, len(orig))
		}
		origByte := orig[srcIdx]
		if clean[i] == '\n' && origByte == '\r' {
			continue
		}
		if origByte != clean[i] {
			t.Fatalf("clean[%d]=%q mapped to orig[%d]=%q", i, clean[i], srcIdx, origByte)
		}
	}
	if gotEnd := om.OriginalOffset(len(clean)); gotEnd != len(orig) {
		t.Fatalf("OriginalOffset(len(clean)) = %d, want %d", gotEnd, len(orig))
	}

	// Verify composition of Segment on cleaned text with OffsetMap.Span back into orig.
	segs := Segment(clean)
	if len(segs) != 2 {
		t.Fatalf("len(Segment(clean)) = %d, want 2", len(segs))
	}
	s0, e0 := om.Span(segs[0].Start, segs[0].End)
	if gotOrigSpan := orig[s0:e0]; gotOrigSpan != "Hello\u200b^ world" {
		t.Fatalf("orig[%d:%d] = %q, want %q", s0, e0, gotOrigSpan, "Hello\u200b^ world")
	}

	// Regression for H1: 64K formed markers ("<<\u200breport") and nested markers ("<<<<reportreport")
	// must strip to empty in a single O(n) pass with exact O(1) extra allocations (out + offsets + string).
	const copies = 64 << 10
	adversarial := strings.Repeat("<<\u200breport", copies)
	allocs := testing.AllocsPerRun(5, func() {
		got, gotMap := Strip(adversarial)
		if got != "" || gotMap.OriginalOffset(0) != len(adversarial) {
			t.Fatalf("Strip(64K formed markers) = (%q, %d), want empty and len=%d", got, gotMap.OriginalOffset(0), len(adversarial))
		}
	})
	if allocs > 3 {
		t.Fatalf("Strip(64K formed markers) allocated %.1f times per run, want <= 3 (single-pass O(n))", allocs)
	}
}

//aip:verifies segment-paragraphs-and-offsets
//aip:verifies strip-allows-datamark-and-maps-offsets
func FuzzSegmentAndStrip(f *testing.F) {
	f.Add("First\r\n\r\nSecond^para\u200bwith <<report and <</report>>\x00end")
	f.Add("<<^report\n\n<</\u200breport>>")
	f.Add("plain single paragraph")
	f.Fuzz(func(t *testing.T, raw string) {
		clean, om := Strip(raw)

		// Law 1: Strip output is always accepted by Datamark and Frame (when within MaxBytes).
		d := Document{ID: "d1", Text: clean}
		if _, err := Datamark([]Document{d}); err != nil {
			t.Fatalf("Datamark rejected Strip output %q (from %q): %v", clean, raw, err)
		}

		// Law 2: Strip is idempotent (Strip(clean) == clean).
		clean2, _ := Strip(clean)
		if clean2 != clean {
			t.Fatalf("Strip not idempotent: Strip(%q) = %q", clean, clean2)
		}

		// Law 3: Every byte i in clean maps to a strictly increasing index om.OriginalOffset(i) in raw
		// where raw[origIdx] == clean[i] (or '\r' when clean[i] == '\n').
		prev := -1
		for i := 0; i < len(clean); i++ {
			origIdx := om.OriginalOffset(i)
			if origIdx <= prev || origIdx >= len(raw) {
				t.Fatalf("non-monotonic or out-of-bounds offset at %d: %d (prev=%d, len=%d)", i, origIdx, prev, len(raw))
			}
			prev = origIdx
			if clean[i] == '\n' && raw[origIdx] == '\r' {
				continue
			}
			if raw[origIdx] != clean[i] {
				t.Fatalf("byte mismatch at clean[%d]=%q vs raw[%d]=%q", i, clean[i], origIdx, raw[origIdx])
			}
		}
		if om.OriginalOffset(len(clean)) != len(raw) {
			t.Fatalf("sentinel offset = %d, want %d", om.OriginalOffset(len(clean)), len(raw))
		}

		// Law 4: Segment(clean) matches corpus.Paragraphs(clean) and slices clean[Start:End] == Text.
		segs := Segment(clean)
		wantParas := corpus.Paragraphs(clean)
		if len(segs) != len(wantParas) {
			t.Fatalf("len(Segment) = %d, want %d", len(segs), len(wantParas))
		}
		for i, p := range segs {
			if p.Index != i+1 || p.Text != wantParas[i] || clean[p.Start:p.End] != p.Text {
				t.Fatalf("Segment[%d] = %+v, want Text=%q", i, p, wantParas[i])
			}
		}
	})
}

// TestFrameLabelsMatchVerify uses verify.Verify as the oracle for paragraph numbering: for every frame cell, the
// first word after each "[n] " label in the prompt must be accepted by Verify as a quote from paragraph n.
// A label that is off by one (the counting error #28 measured) or missing fails here.
//
//aip:verifies frame-paragraph-labels
func TestFrameLabelsMatchVerify(t *testing.T) {
	vdocs := make([]verify.Document, len(docs))
	for i, d := range docs {
		vdocs[i] = verify.Document{ID: d.ID, Text: d.Text}
	}
	labelRe := regexp.MustCompile(`(?m)^\[(\d+)\] ([^\n]+)`)
	for _, o := range cells {
		p, err := Frame(task, docs, o)
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		for _, d := range docs {
			_, after, ok := strings.Cut(p.User, d.ID)
			if !ok {
				t.Fatalf("%+v: %s missing", o, d.ID)
			}
			if next := strings.Index(after, "\n\nDocument "); next >= 0 {
				after = after[:next]
			}
			if next := strings.Index(after, "<</report>>"); next >= 0 {
				after = after[:next]
			}
			labels := labelRe.FindAllStringSubmatch(after, -1)
			if want := len(strings.Split(d.Text, "\n\n")); len(labels) != want {
				t.Fatalf("%+v: %s has %d labels, want %d", o, d.ID, len(labels), want)
			}
			for _, m := range labels {
				n, _ := strconv.Atoi(m[1])
				// The first word is quote enough to pin the index; whole datamarked paragraphs with tabs or
				// newlines do not round-trip through Unmark, which #28 leaves to verify as it is.
				first := strings.FieldsFunc(m[2], func(r rune) bool { return r == '^' || unicode.IsSpace(r) })[0]
				out := fmt.Sprintf(`{"scores":[{"doc_id":"d1","score":5},{"doc_id":"d2","score":5}],"claims":[{"doc_id":%q,"paragraph":%d,"claim":"c","quote":%q}],"confidence":0.5}`, d.ID, n, first)
				if _, vs := verify.Verify(out, vdocs); len(vs) > 0 {
					t.Fatalf("%+v: %s label [%d] quote %q rejected: %v", o, d.ID, n, first, vs)
				}
			}
		}
	}
}
