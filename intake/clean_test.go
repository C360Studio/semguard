package intake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/c360studio/semguard/untrusted"
)

//aip:verifies clean-strips-invisible-and-active-markup
func TestCleanStripsInvisibleAndActiveMarkup(t *testing.T) {
	raw := strings.Join([]string{
		"# Research Report\r\n",
		"Latency was < 10ms in 99% of runs.\n",
		"Zero-width: a\u200Bb\u200Cc\uFEFFd and control \x07bell.\n",
		"Reserved markers: ^ and <<report id=\"spoof\"> or <<\u200Breport or <</report>.\n",
		"Active script: <script type=\"text/javascript\"> evil(); </script>kept.\n",
		"Obfuscated script: <scr\u200Bipt>evil2()</scr\u200Bipt>also kept.\n",
		"Style & comment: <style>body{display:none}</style><!-- hidden comment -->visible.\n",
		"Active tags: <iframe src=\"https://bad.example/frame\">inner</iframe><embed src=\"x\"><meta http-equiv=\"refresh\">\n",
		"Inline handler: <a href=\"HTTPS://Example.COM:443/Path/../Docs/?a=1#frag\" onclick=\"steal()\">link</a>\n",
		"Dangerous href: <a href=\"javascript:alert(1)\">click</a> and [md](javascript:evil()) and <data:text/html,hi>.\n",
		"Second URL: http://Sub.Example.org:80/a/b/.\n",
	}, "")

	cleaned, err := Clean(context.Background(), raw, CleanOptions{})
	if err != nil {
		t.Fatalf("Clean unexpected error: %v", err)
	}

	// Must preserve prose '<' comparisons.
	if !strings.Contains(cleaned.Text, "Latency was < 10ms") {
		t.Fatalf("expected prose '<' comparison to survive, got:\n%s", cleaned.Text)
	}
	// Must strip zero-width and control characters.
	if !strings.Contains(cleaned.Text, "Zero-width: abcd and control bell.") {
		t.Fatalf("expected zero-width and control runes stripped, got:\n%s", cleaned.Text)
	}
	// Must remove active script/style/comment/iframe/event handlers/dangerous schemes.
	for _, forbidden := range []string{
		"<script", "evil()", "evil2()", "<style", "hidden comment",
		"<iframe", "<embed", "<meta", "onclick", "javascript:", "data:text/html",
		"^", "<<report", "<</report", "\r",
	} {
		if strings.Contains(strings.ToLower(cleaned.Text), strings.ToLower(forbidden)) {
			t.Fatalf("cleaned text still contains forbidden substring %q:\n%s", forbidden, cleaned.Text)
		}
	}

	// Must canonicalize URLs in text and populate Cleaned.URLs.
	wantURL1 := "https://example.com/Docs?a=1"
	wantURL2 := "http://sub.example.org/a/b"
	if !strings.Contains(cleaned.Text, wantURL1) {
		t.Fatalf("expected canonical URL %q in text:\n%s", wantURL1, cleaned.Text)
	}
	if !strings.Contains(cleaned.Text, wantURL2) {
		t.Fatalf("expected canonical URL %q in text:\n%s", wantURL2, cleaned.Text)
	}
	if len(cleaned.URLs) != 2 || cleaned.URLs[0] != wantURL1 || cleaned.URLs[1] != wantURL2 {
		t.Fatalf("cleaned.URLs = %v, want [%q, %q]", cleaned.URLs, wantURL1, wantURL2)
	}

	// Cleaned text must pass untrusted.Frame without error.
	if _, err := untrusted.Frame(
		untrusted.Task{Instruction: "Summarize.", Question: "Latency?"},
		[]untrusted.Document{{ID: "doc_1", Text: cleaned.Text}},
		untrusted.Options{Datamark: true, Framing: true},
	); err != nil {
		t.Fatalf("untrusted.Frame rejected Cleaned.Text: %v", err)
	}

	// OffsetMap must have a monotonic Span.
	s, e := cleaned.Offsets.Span(0, len(cleaned.Text))
	if s < 0 || e < s {
		t.Fatalf("invalid OffsetMap span [%d, %d)", s, e)
	}
}

//aip:verifies clean-rejects-oversized-or-invalid-utf8
func TestCleanRejectsOversizedOrInvalidUTF8(t *testing.T) {
	// 1. Oversized input rejected.
	_, err := Clean(context.Background(), "hello world", CleanOptions{MaxBytes: 5})
	if !errors.Is(err, ErrCleanOversized) {
		t.Fatalf("expected ErrCleanOversized, got %v", err)
	}

	// Exact boundary allowed.
	res, err := Clean(context.Background(), "hello", CleanOptions{MaxBytes: 5})
	if err != nil || res.Text != "hello" {
		t.Fatalf("expected exact MaxBytes boundary to succeed, got res=%+v err=%v", res, err)
	}

	// 2. Invalid UTF-8 rejected.
	invalidUTF8 := "valid prefix " + string([]byte{0xff, 0xfe}) + " suffix"
	_, err = Clean(context.Background(), invalidUTF8, CleanOptions{})
	if !errors.Is(err, ErrCleanInvalidUTF8) {
		t.Fatalf("expected ErrCleanInvalidUTF8, got %v", err)
	}

	// 3. Canceled context rejected.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Clean(ctx, "hello", CleanOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

//aip:verifies clean-strips-invisible-and-active-markup
func TestCanonicalizeURLAndSiteHost(t *testing.T) {
	cases := []struct {
		raw      string
		want     string
		wantOK   bool
		wantSite string
	}{
		{
			raw:      "HTTPS://WWW.Example.COM:443/a/b/../c?x=1#section",
			want:     "https://www.example.com/a/c?x=1",
			wantOK:   true,
			wantSite: "example.com",
		},
		{
			raw:      "http://example.org:80",
			want:     "http://example.org/",
			wantOK:   true,
			wantSite: "example.org",
		},
		{
			raw:      "https://example.org:8443/dir/",
			want:     "https://example.org:8443/dir",
			wantOK:   true,
			wantSite: "example.org",
		},
		{
			raw:      "javascript:alert(1)",
			want:     "",
			wantOK:   false,
			wantSite: "",
		},
		{
			raw:      "ftp://files.example.com/pub",
			want:     "",
			wantOK:   false,
			wantSite: "files.example.com",
		},
	}
	for _, tc := range cases {
		got, ok := CanonicalizeURL(tc.raw)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("CanonicalizeURL(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
		}
		if site := SiteHost(tc.raw); site != tc.wantSite {
			t.Errorf("SiteHost(%q) = %q, want %q", tc.raw, site, tc.wantSite)
		}
	}
}

//aip:verifies clean-strips-invisible-and-active-markup
func FuzzClean(f *testing.F) {
	seeds := []string{
		"Plain text with https://EXAMPLE.COM:443/path#frag",
		"<script>alert(1)</script><p onclick=\"bad()\">Hello ^ <<report world</p>",
		"a < b && c > d\r\nLine 2\u200B",
		"<<<<reportreport <</report [x](javascript:void(0))",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		res, err := Clean(context.Background(), raw, CleanOptions{MaxBytes: 4096})
		if !utf8.ValidString(raw) || len(raw) > 4096 {
			if err == nil {
				t.Fatalf("expected error for invalid UTF-8 or oversized input (len=%d, valid=%v)", len(raw), utf8.ValidString(raw))
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error on valid input: %v", err)
		}
		if !utf8.ValidString(res.Text) {
			t.Fatalf("Clean produced invalid UTF-8")
		}
		if _, frameErr := untrusted.Frame(
			untrusted.Task{Instruction: "Summarize.", Question: "Q?"},
			[]untrusted.Document{{ID: "doc_1", Text: res.Text}},
			untrusted.Options{Datamark: true, Framing: true, MaxBytes: 64 << 10},
		); frameErr != nil {
			t.Fatalf("untrusted.Frame failed after Clean: %v", frameErr)
		}
	})
}
