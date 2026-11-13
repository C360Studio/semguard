package intake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/c360studio/semguard/untrusted"
)

// DefaultMaxCleanBytes is the default maximum byte size accepted by Clean (1 MiB).
const DefaultMaxCleanBytes = 1 << 20

var (
	// ErrCleanOversized is returned when input to Clean exceeds MaxBytes.
	ErrCleanOversized = errors.New("intake: input exceeds maximum byte size")
	// ErrCleanInvalidUTF8 is returned when input to Clean contains invalid UTF-8 bytes.
	ErrCleanInvalidUTF8 = errors.New("intake: input contains invalid UTF-8")
)

// CleanOptions configures Clean.
type CleanOptions struct {
	// MaxBytes is the maximum input length in bytes. If <= 0, DefaultMaxCleanBytes is used.
	MaxBytes int
}

// Cleaned holds the result of Clean.
type Cleaned struct {
	// Text is the cleaned, UTF-8-valid string with active markup stripped,
	// URLs canonicalized, and untrusted.Strip applied (safe for untrusted.Frame).
	Text string
	// Offsets maps byte offsets in Text back to the post-markup/URL-canonicalized
	// string passed into untrusted.Strip.
	Offsets untrusted.OffsetMap
	// URLs is the ordered list of deduplicated canonical http/https URLs found in Text.
	URLs []string
}

var (
	// Paired active container blocks removed along with their inner content.
	scriptBlockRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	styleBlockRe  = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`)
	svgBlockRe    = regexp.MustCompile(`(?is)<svg\b[^>]*>.*?</svg\s*>`)
	iframeBlockRe = regexp.MustCompile(`(?is)<iframe\b[^>]*>.*?</iframe>`)
	objectBlockRe = regexp.MustCompile(`(?is)<object\b[^>]*>.*?</object\s*>`)
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

	// Standalone/unclosed active tags.
	activeTagRe = regexp.MustCompile(`(?is)</?\s*(?:script|style|iframe|object|embed|svg|form|meta|link|base|applet)\b[^>]*>`)

	// Inline event-handler attributes (e.g. onclick="...", onload='...', onerror=alert(1)) inside HTML tags.
	htmlTagWithAttrRe = regexp.MustCompile(`(?is)<[a-zA-Z][^>]*>`)
	eventAttrRe       = regexp.MustCompile(`(?is)\s+on[a-z0-9_]+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>"']+)`)
	dangerAttrURLRe   = regexp.MustCompile(`(?is)\s+(?:href|src|xlink:href|action|formaction|data)\s*=\s*(?:"\s*(?:javascript|vbscript|data):[^"]*"|'\s*(?:javascript|vbscript|data):[^']*'|(?:\s*(?:javascript|vbscript|data):[^\s>"']+))`)

	// Dangerous scheme links in Markdown `[label](javascript:...)` or autolinks `<javascript:...>`.
	mdDangerLinkRe   = regexp.MustCompile(`(?is)\]\(\s*(?:javascript|vbscript|data):[^)]*\)`)
	autoDangerLinkRe = regexp.MustCompile(`(?is)<\s*(?:javascript|vbscript|data):[^>]*>`)

	// HTTP/HTTPS URL matcher (case-insensitive scheme; stops before common trailing delimiters).
	rawURLRe = regexp.MustCompile(`(?i)https?://[^\s"'<>)\]]+`)
)

// Clean validates UTF-8 and size bounds, removes active HTML/script/style markup
// and event-handler attributes while preserving Markdown and prose '<' comparisons,
// canonicalizes http/https URLs to a single form, and runs untrusted.Strip so
// invisible/control characters and reserved datamark tokens ("^", "<<report", "<</report")
// are removed before any downstream splitting or model framing.
func Clean(ctx context.Context, raw string, opts CleanOptions) (Cleaned, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Cleaned{}, err
		}
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCleanBytes
	}
	if len(raw) > maxBytes {
		return Cleaned{}, fmt.Errorf("%w: %d > %d", ErrCleanOversized, len(raw), maxBytes)
	}
	if !utf8.ValidString(raw) {
		return Cleaned{}, ErrCleanInvalidUTF8
	}

	// Step 1: Strip invisible format/control characters and reserved datamark tokens first
	// so obfuscated tags like "<scr\u200Bipt>" or "java\u200Bscript:" collapse before markup removal,
	// and then run a second Strip pass after markup and URL canonicalization to guarantee
	// no stripped sequence could be reassembled by tag removal.
	preStripped, _ := untrusted.Strip(raw)
	noMarkup := stripActiveMarkup(preStripped)
	canonText, urls := canonicalizeURLsInText(noMarkup)
	cleanedText, offsets := untrusted.Strip(canonText)

	return Cleaned{
		Text:    cleanedText,
		Offsets: offsets,
		URLs:    urls,
	}, nil
}

func stripActiveMarkup(s string) string {
	s = htmlCommentRe.ReplaceAllString(s, "")
	s = scriptBlockRe.ReplaceAllString(s, "")
	s = styleBlockRe.ReplaceAllString(s, "")
	s = svgBlockRe.ReplaceAllString(s, "")
	s = iframeBlockRe.ReplaceAllString(s, "")
	s = objectBlockRe.ReplaceAllString(s, "")
	s = activeTagRe.ReplaceAllString(s, "")
	s = htmlTagWithAttrRe.ReplaceAllStringFunc(s, func(tag string) string {
		tag = eventAttrRe.ReplaceAllString(tag, "")
		tag = dangerAttrURLRe.ReplaceAllString(tag, "")
		return tag
	})
	s = mdDangerLinkRe.ReplaceAllString(s, "]()")
	s = autoDangerLinkRe.ReplaceAllString(s, "")
	return s
}

func canonicalizeURLsInText(s string) (string, []string) {
	var seen map[string]bool
	var urls []string
	out := rawURLRe.ReplaceAllStringFunc(s, func(match string) string {
		core, suffix := splitTrailingURLPunct(match)
		canon, ok := CanonicalizeURL(core)
		if !ok {
			return match
		}
		if seen == nil {
			seen = make(map[string]bool)
		}
		if !seen[canon] {
			seen[canon] = true
			urls = append(urls, canon)
		}
		return canon + suffix
	})
	return out, urls
}

func splitTrailingURLPunct(u string) (core, suffix string) {
	end := len(u)
	for end > 0 {
		switch u[end-1] {
		case '.', ',', ';', ':', '!', '?':
			end--
		default:
			return u[:end], u[end:]
		}
	}
	return u, ""
}

// CanonicalizeURL normalizes an http:// or https:// URL into a single canonical form:
// lowercase scheme and host, default port (:80 for http, :443 for https) removed,
// trailing dot on host removed, fragment (#...) removed, and path cleaned with a
// leading "/" when empty. Returns ("", false) if rawURL is not a valid http/https URL.
func CanonicalizeURL(rawURL string) (string, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", false
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if u.Host == "" {
		return "", false
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", false
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	u.Scheme = scheme
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
	}

	u.Fragment = ""
	u.RawFragment = ""

	if u.Path == "" {
		u.Path = "/"
	} else {
		cleaned := path.Clean(u.Path)
		if !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		u.Path = cleaned
	}
	u.RawPath = ""

	return u.String(), true
}

// SiteHost returns the canonical registrable-like site host for a URL
// (lowercase hostname with leading "www." and trailing "." stripped).
func SiteHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	h = strings.TrimPrefix(h, "www.")
	return h
}
