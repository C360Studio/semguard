package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaxParseBytes is the default maximum byte size accepted by ParseExaSearch
// and ParseExaContents (4 MiB).
const DefaultMaxParseBytes = 4 << 20

var (
	// ErrParseOversized is returned when a response body exceeds ParseOptions.MaxBytes.
	ErrParseOversized = errors.New("intake: response body exceeds maximum byte size")
	// ErrParseShape is returned when a response body is malformed JSON, carries unknown fields,
	// or is missing required Exa wire fields.
	ErrParseShape = errors.New("intake: unexpected Exa response shape")
)

// ParseOptions configures Exa response parsing.
type ParseOptions struct {
	// MaxBytes is the maximum response body size in bytes. If <= 0, DefaultMaxParseBytes is used.
	MaxBytes int
}

// ExaCostSearch holds the search cost breakdown in costDollars.
type ExaCostSearch struct {
	Neural float64 `json:"neural"`
}

// ExaCostDollars holds the per-request cost breakdown.
type ExaCostDollars struct {
	Total  float64        `json:"total"`
	Search *ExaCostSearch `json:"search,omitempty"`
}

// ExaExtras holds optional per-result extras returned by Exa (such as outbound links).
type ExaExtras struct {
	Links      []string `json:"links,omitempty"`
	ImageLinks []string `json:"imageLinks,omitempty"`
}

// ExaResult is one parsed Exa result from POST /search or POST /contents.
type ExaResult struct {
	Title           string
	URL             string
	CanonicalURL    string
	Site            string
	ID              string
	PublishedDate   *string
	Author          *string
	Text            string
	Highlights      []string
	HighlightScores []float64
	Summary         string
	Image           string
	Favicon         string
	// Links is the list of outbound links reported for the result (from extras.links or links),
	// canonicalized where valid http/https URLs.
	Links []string
	// LinksPresent reports whether the wire result included an outbound link list
	// (either results[].extras.links or results[].links), even if that list was empty.
	LinksPresent bool
}

// ExaContentsStatusError is the error object on a POST /contents status entry.
type ExaContentsStatusError struct {
	Tag            string `json:"tag"`
	HTTPStatusCode *int   `json:"httpStatusCode"`
}

// ExaContentsStatus is one element of POST /contents statuses[].
type ExaContentsStatus struct {
	ID     string                  `json:"id"`
	Status string                  `json:"status"`
	Source string                  `json:"source,omitempty"`
	Error  *ExaContentsStatusError `json:"error,omitempty"`
}

// ExaGroundingCitation is one citation backing a structured output field.
type ExaGroundingCitation struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	ID    string `json:"id,omitempty"`
}

// ExaGrounding ties one field of structured output to its supporting citations.
type ExaGrounding struct {
	Field      string                 `json:"field"`
	Citations  []ExaGroundingCitation `json:"citations,omitempty"`
	Confidence string                 `json:"confidence,omitempty"`
}

// ExaStructuredOutput is the optional structured-output branch of POST /search.
type ExaStructuredOutput struct {
	Content   json.RawMessage `json:"content,omitempty"`
	Grounding []ExaGrounding  `json:"grounding,omitempty"`
}

// ExaSearchResponse is the parsed POST /search response.
type ExaSearchResponse struct {
	RequestID          string
	Results            []ExaResult
	CostDollars        ExaCostDollars
	ResolvedSearchType *string
	Context            *string
	Output             *ExaStructuredOutput
}

// ExaContentsResponse is the parsed POST /contents response.
type ExaContentsResponse struct {
	RequestID   string
	Results     []ExaResult
	Statuses    []ExaContentsStatus
	CostDollars ExaCostDollars
}

// Wire decoding structs with pointer fields to detect missing required top-level keys
// while DisallowUnknownFields rejects unexpected keys.
type exaSearchWire struct {
	RequestID          *string              `json:"requestId"`
	Results            *[]exaResultWire     `json:"results"`
	CostDollars        *exaCostDollarsWire  `json:"costDollars"`
	ResolvedSearchType *string              `json:"resolvedSearchType,omitempty"`
	Context            *string              `json:"context,omitempty"`
	Output             *ExaStructuredOutput `json:"output,omitempty"`
}

type exaContentsWire struct {
	RequestID   *string                  `json:"requestId"`
	Results     *[]exaResultWire         `json:"results"`
	Statuses    *[]exaContentsStatusWire `json:"statuses"`
	CostDollars *exaCostDollarsWire      `json:"costDollars"`
}

type exaCostDollarsWire struct {
	Total  *float64       `json:"total"`
	Search *ExaCostSearch `json:"search,omitempty"`
}

type exaExtrasWire struct {
	Links      *[]string `json:"links,omitempty"`
	ImageLinks *[]string `json:"imageLinks,omitempty"`
}

type exaResultWire struct {
	Title           string          `json:"title"`
	URL             *string         `json:"url"`
	ID              string          `json:"id,omitempty"`
	PublishedDate   *string         `json:"publishedDate"`
	Author          *string         `json:"author"`
	Text            string          `json:"text,omitempty"`
	Highlights      []string        `json:"highlights,omitempty"`
	HighlightScores []float64       `json:"highlightScores,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	Image           string          `json:"image,omitempty"`
	Favicon         string          `json:"favicon,omitempty"`
	Extras          *exaExtrasWire  `json:"extras,omitempty"`
	Links           *[]string       `json:"links,omitempty"`
	Subpages        json.RawMessage `json:"subpages,omitempty"`
}

type exaContentsStatusWire struct {
	ID     *string                 `json:"id"`
	Status *string                 `json:"status"`
	Source string                  `json:"source,omitempty"`
	Error  *ExaContentsStatusError `json:"error,omitempty"`
}

// ParseExaSearch strictly decodes an Exa POST /search JSON response body.
func ParseExaSearch(ctx context.Context, body []byte, opts ParseOptions) (ExaSearchResponse, error) {
	if err := checkParsePreconditions(ctx, body, opts); err != nil {
		return ExaSearchResponse{}, err
	}
	var w exaSearchWire
	if err := decodeStrictJSON(body, &w); err != nil {
		return ExaSearchResponse{}, fmt.Errorf("%w: %v", ErrParseShape, err)
	}
	if w.RequestID == nil || strings.TrimSpace(*w.RequestID) == "" {
		return ExaSearchResponse{}, fmt.Errorf("%w: missing requestId", ErrParseShape)
	}
	if w.Results == nil {
		return ExaSearchResponse{}, fmt.Errorf("%w: missing results array", ErrParseShape)
	}
	if w.CostDollars == nil || w.CostDollars.Total == nil {
		return ExaSearchResponse{}, fmt.Errorf("%w: missing costDollars.total", ErrParseShape)
	}
	results, err := convertExaResults(*w.Results)
	if err != nil {
		return ExaSearchResponse{}, err
	}
	return ExaSearchResponse{
		RequestID: *w.RequestID,
		Results:   results,
		CostDollars: ExaCostDollars{
			Total:  *w.CostDollars.Total,
			Search: w.CostDollars.Search,
		},
		ResolvedSearchType: w.ResolvedSearchType,
		Context:            w.Context,
		Output:             w.Output,
	}, nil
}

// ParseExaContents strictly decodes an Exa POST /contents JSON response body.
func ParseExaContents(ctx context.Context, body []byte, opts ParseOptions) (ExaContentsResponse, error) {
	if err := checkParsePreconditions(ctx, body, opts); err != nil {
		return ExaContentsResponse{}, err
	}
	var w exaContentsWire
	if err := decodeStrictJSON(body, &w); err != nil {
		return ExaContentsResponse{}, fmt.Errorf("%w: %v", ErrParseShape, err)
	}
	if w.RequestID == nil || strings.TrimSpace(*w.RequestID) == "" {
		return ExaContentsResponse{}, fmt.Errorf("%w: missing requestId", ErrParseShape)
	}
	if w.Results == nil {
		return ExaContentsResponse{}, fmt.Errorf("%w: missing results array", ErrParseShape)
	}
	if w.Statuses == nil {
		return ExaContentsResponse{}, fmt.Errorf("%w: missing statuses array", ErrParseShape)
	}
	if w.CostDollars == nil || w.CostDollars.Total == nil {
		return ExaContentsResponse{}, fmt.Errorf("%w: missing costDollars.total", ErrParseShape)
	}
	results, err := convertExaResults(*w.Results)
	if err != nil {
		return ExaContentsResponse{}, err
	}
	statuses := make([]ExaContentsStatus, len(*w.Statuses))
	for i, sw := range *w.Statuses {
		if sw.ID == nil || strings.TrimSpace(*sw.ID) == "" {
			return ExaContentsResponse{}, fmt.Errorf("%w: statuses[%d]: missing id", ErrParseShape, i)
		}
		if sw.Status == nil || strings.TrimSpace(*sw.Status) == "" {
			return ExaContentsResponse{}, fmt.Errorf("%w: statuses[%d]: missing status", ErrParseShape, i)
		}
		statuses[i] = ExaContentsStatus{
			ID:     *sw.ID,
			Status: *sw.Status,
			Source: sw.Source,
			Error:  sw.Error,
		}
	}
	return ExaContentsResponse{
		RequestID: *w.RequestID,
		Results:   results,
		Statuses:  statuses,
		CostDollars: ExaCostDollars{
			Total:  *w.CostDollars.Total,
			Search: w.CostDollars.Search,
		},
	}, nil
}

func checkParsePreconditions(ctx context.Context, body []byte, opts ParseOptions) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxParseBytes
	}
	if len(body) > maxBytes {
		return fmt.Errorf("%w: %d > %d", ErrParseOversized, len(body), maxBytes)
	}
	return nil
}

func decodeStrictJSON(body []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON tokens after root object")
		}
		return err
	}
	return nil
}

func convertExaResults(wires []exaResultWire) ([]ExaResult, error) {
	out := make([]ExaResult, len(wires))
	for i, rw := range wires {
		if rw.URL == nil || strings.TrimSpace(*rw.URL) == "" {
			return nil, fmt.Errorf("%w: results[%d]: missing url", ErrParseShape, i)
		}
		rawURL := strings.TrimSpace(*rw.URL)
		canonURL, ok := CanonicalizeURL(rawURL)
		if !ok {
			return nil, fmt.Errorf("%w: results[%d]: invalid http/https url %q", ErrParseShape, i, rawURL)
		}
		id := strings.TrimSpace(rw.ID)
		if id == "" {
			id = rawURL
		}

		var rawLinks []string
		linksPresent := false
		if rw.Extras != nil && rw.Extras.Links != nil {
			linksPresent = true
			rawLinks = append(rawLinks, (*rw.Extras.Links)...)
		}
		if rw.Links != nil {
			linksPresent = true
			rawLinks = append(rawLinks, (*rw.Links)...)
		}
		var links []string
		if linksPresent {
			links = make([]string, 0, len(rawLinks))
			seen := make(map[string]bool, len(rawLinks))
			for _, l := range rawLinks {
				if c, ok := CanonicalizeURL(l); ok {
					if !seen[c] {
						seen[c] = true
						links = append(links, c)
					}
				} else if trimmed := strings.TrimSpace(l); trimmed != "" && !seen[trimmed] {
					seen[trimmed] = true
					links = append(links, trimmed)
				}
			}
		}

		out[i] = ExaResult{
			Title:           rw.Title,
			URL:             rawURL,
			CanonicalURL:    canonURL,
			Site:            SiteHost(canonURL),
			ID:              id,
			PublishedDate:   rw.PublishedDate,
			Author:          rw.Author,
			Text:            rw.Text,
			Highlights:      rw.Highlights,
			HighlightScores: rw.HighlightScores,
			Summary:         rw.Summary,
			Image:           rw.Image,
			Favicon:         rw.Favicon,
			Links:           links,
			LinksPresent:    linksPresent,
		}
	}
	return out, nil
}
