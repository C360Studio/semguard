package intake

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EntityType identifies the kind of entity in the in-memory intake graph.
type EntityType string

// EntityType values for the six intake graph node kinds.
const (
	EntityQuestion EntityType = "question"
	EntityProvider EntityType = "provider"
	EntityReport   EntityType = "report"
	EntityPassage  EntityType = "passage"
	EntitySource   EntityType = "source"
	EntitySite     EntityType = "site"
)

// Predicate Constants for intake graph edges.
const (
	PredicateBelongsTo = "intake.relation.belongs_to"
	PredicateCites     = "intake.relation.cites"
	PredicateType      = "intake.entity.type"
	PredicateText      = "intake.entity.text"
	PredicateTitle     = "intake.entity.title"
	PredicateURL       = "intake.entity.url"
	PredicateSite      = "intake.entity.site"
)

// Graphable mirrors semengine/graph.Graphable without pulling in NATS/container dependencies.
type Graphable interface {
	EntityID() string
	Triples() []Triple
}

// Triple mirrors semengine/message.Triple (SPO with provenance and confidence).
type Triple struct {
	Subject    string    `json:"subject"`
	Predicate  string    `json:"predicate"`
	Object     any       `json:"object"`
	Source     string    `json:"source"`
	Timestamp  time.Time `json:"timestamp"`
	Confidence float64   `json:"confidence"`
}

// EntityState mirrors semengine/graph.EntityState for in-memory entity representation.
type EntityState struct {
	ID        string     `json:"id"`
	Type      EntityType `json:"type"`
	Triples   []Triple   `json:"triples"`
	Version   int64      `json:"version"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// EntityID implements Graphable.
func (e EntityState) EntityID() string { return e.ID }

// Report is one cleaned and split provider result ready for graph construction and passage checks.
type Report struct {
	ID              string
	Title           string
	URL             string
	CanonicalURL    string
	Site            string
	PublishedDate   *string
	Author          *string
	CleanedText     string
	TextURLs        []string
	Links           []string
	LinksPresent    bool
	Highlights      []string
	HighlightScores []float64
	Summary         string
	Passages        []Passage
}

// Graph holds the in-memory entities and triples built from one intake batch.
type Graph struct {
	QuestionID string
	Question   string
	ProviderID string
	Provider   string
	Entities   map[string]*EntityState
	Order      []string
	Triples    []Triple
}

// EntitiesByType returns all EntityStates in insertion order matching the given EntityType.
func (g *Graph) EntitiesByType(t EntityType) []*EntityState {
	var out []*EntityState
	for _, id := range g.Order {
		if e := g.Entities[id]; e != nil && e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// Edges returns all relationship triples matching the given predicate (e.g. PredicateBelongsTo or PredicateCites).
func (g *Graph) Edges(predicate string) []Triple {
	var out []Triple
	for _, tr := range g.Triples {
		if tr.Predicate == predicate {
			out = append(out, tr)
		}
	}
	return out
}

// PrepareExaReports cleans and splits each ExaResult into a Report.
func PrepareExaReports(results []ExaResult, cleanOpts CleanOptions) ([]Report, error) {
	reports := make([]Report, len(results))
	for i, r := range results {
		docID := r.ID
		if strings.TrimSpace(docID) == "" {
			docID = fmt.Sprintf("doc_%d", i+1)
		}
		cleaned, err := Clean(context.Background(), r.Text, cleanOpts)
		if err != nil {
			return nil, fmt.Errorf("results[%d] (%s): %w", i, docID, err)
		}
		passages := Split(docID, cleaned.Text)
		canonURL := r.CanonicalURL
		if canonURL == "" {
			if c, ok := CanonicalizeURL(r.URL); ok {
				canonURL = c
			} else {
				canonURL = r.URL
			}
		}
		site := r.Site
		if site == "" {
			site = SiteHost(canonURL)
		}
		reports[i] = Report{
			ID:              docID,
			Title:           r.Title,
			URL:             r.URL,
			CanonicalURL:    canonURL,
			Site:            site,
			PublishedDate:   r.PublishedDate,
			Author:          r.Author,
			CleanedText:     cleaned.Text,
			TextURLs:        cleaned.URLs,
			Links:           r.Links,
			LinksPresent:    r.LinksPresent,
			Highlights:      r.Highlights,
			HighlightScores: r.HighlightScores,
			Summary:         r.Summary,
			Passages:        passages,
		}
	}
	return reports, nil
}

// BuildGraph constructs the in-memory intake Graph from a question, provider name, and prepared Reports.
//
// Entities created:
//   - question ("intake.question.1")
//   - provider ("intake.provider.<providerName>")
//   - report ("intake.report.<index>")
//   - passage (each Passage.ID, e.g. "<docID>#p<ordinal>")
//   - source ("intake.source.<canonicalURL>")
//   - site ("intake.site.<siteHost>")
//
// Edges created:
//   - passage belongs_to report
//   - report belongs_to provider
//   - report belongs_to question
//   - source belongs_to site
//   - report cites source (its own source URL and each declared outbound link)
//   - passage cites source (each canonical URL appearing inside the passage body)
func BuildGraph(question, providerName string, reports []Report) *Graph {
	now := time.Now().UTC()
	if strings.TrimSpace(providerName) == "" {
		providerName = "exa"
	}
	g := &Graph{
		QuestionID: "intake.question.1",
		Question:   question,
		ProviderID: "intake.provider." + strings.ToLower(strings.TrimSpace(providerName)),
		Provider:   providerName,
		Entities:   make(map[string]*EntityState),
	}

	ensureEntity := func(id string, typ EntityType) *EntityState {
		if e, ok := g.Entities[id]; ok {
			return e
		}
		e := &EntityState{
			ID:        id,
			Type:      typ,
			Version:   1,
			UpdatedAt: now,
		}
		g.Entities[id] = e
		g.Order = append(g.Order, id)
		addTriple(g, e, PredicateType, string(typ), providerName, now)
		return e
	}

	qEnt := ensureEntity(g.QuestionID, EntityQuestion)
	addTriple(g, qEnt, PredicateText, question, providerName, now)

	pEnt := ensureEntity(g.ProviderID, EntityProvider)
	addTriple(g, pEnt, PredicateTitle, providerName, providerName, now)

	ensureSourceAndSite := func(rawOrCanonURL string) string {
		canon, ok := CanonicalizeURL(rawOrCanonURL)
		if !ok {
			canon = strings.TrimSpace(rawOrCanonURL)
		}
		if canon == "" {
			return ""
		}
		srcID := "intake.source." + canon
		srcEnt := ensureEntity(srcID, EntitySource)
		addTripleUnique(g, srcEnt, PredicateURL, canon, providerName, now)

		siteHost := SiteHost(canon)
		if siteHost != "" {
			siteID := "intake.site." + siteHost
			siteEnt := ensureEntity(siteID, EntitySite)
			addTripleUnique(g, siteEnt, PredicateSite, siteHost, providerName, now)
			addTripleUnique(g, srcEnt, PredicateBelongsTo, siteID, providerName, now)
		}
		return srcID
	}

	for i, rep := range reports {
		repEntityID := fmt.Sprintf("intake.report.%d", i+1)
		repEnt := ensureEntity(repEntityID, EntityReport)
		addTriple(g, repEnt, PredicateTitle, rep.Title, providerName, now)
		addTriple(g, repEnt, PredicateURL, rep.CanonicalURL, providerName, now)
		addTriple(g, repEnt, PredicateBelongsTo, g.ProviderID, providerName, now)
		addTriple(g, repEnt, PredicateBelongsTo, g.QuestionID, providerName, now)

		if selfSrcID := ensureSourceAndSite(rep.CanonicalURL); selfSrcID != "" {
			addTripleUnique(g, repEnt, PredicateCites, selfSrcID, providerName, now)
		}
		for _, link := range rep.Links {
			if linkSrcID := ensureSourceAndSite(link); linkSrcID != "" {
				addTripleUnique(g, repEnt, PredicateCites, linkSrcID, providerName, now)
			}
		}

		for _, psg := range rep.Passages {
			psgEnt := ensureEntity(psg.ID, EntityPassage)
			addTriple(g, psgEnt, PredicateText, psg.Body, providerName, now)
			addTriple(g, psgEnt, PredicateBelongsTo, repEntityID, providerName, now)

			_, psgURLs := canonicalizeURLsInText(psg.Body)
			for _, u := range psgURLs {
				if citedSrcID := ensureSourceAndSite(u); citedSrcID != "" {
					addTripleUnique(g, psgEnt, PredicateCites, citedSrcID, providerName, now)
				}
			}
		}
	}

	return g
}

func addTriple(g *Graph, ent *EntityState, predicate string, object any, source string, ts time.Time) {
	tr := Triple{
		Subject:    ent.ID,
		Predicate:  predicate,
		Object:     object,
		Source:     source,
		Timestamp:  ts,
		Confidence: 1.0,
	}
	ent.Triples = append(ent.Triples, tr)
	g.Triples = append(g.Triples, tr)
}

func addTripleUnique(g *Graph, ent *EntityState, predicate string, object any, source string, ts time.Time) {
	for _, existing := range ent.Triples {
		if existing.Predicate == predicate && existing.Object == object {
			return
		}
	}
	addTriple(g, ent, predicate, object, source, ts)
}
