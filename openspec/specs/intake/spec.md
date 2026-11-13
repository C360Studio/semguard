# intake Specification

## Purpose
TBD - created by archiving change exa-intake-path. Update Purpose after archive.
## Requirements
### Requirement: Clean input text and canonicalize URLs

`Clean` SHALL honor its `context.Context`, reject inputs larger than `MaxBytes` or containing invalid UTF-8 with an error, remove active HTML/script/style markup and event-handler attributes, canonicalize `http`/`https` URLs to a single form, and apply `untrusted.Strip` so invisible/control characters and reserved datamark tokens (`^`, `<<report`, `<</report`) are removed and an `OffsetMap` is returned.

#### Scenario: clean-strips-invisible-and-active-markup

- **WHEN** `Clean` receives valid UTF-8 text containing zero-width format characters, `<script>` markup, reserved `<<report` markers, and non-canonical URLs
- **THEN** it returns cleaned text safe for `untrusted.Frame`, canonicalized URLs, and a valid `OffsetMap`

#### Scenario: clean-rejects-oversized-or-invalid-utf8

- **WHEN** `Clean` receives input exceeding `MaxBytes` or containing invalid UTF-8 bytes
- **THEN** it returns an error and no cleaned text

### Requirement: Parse Exa search and contents responses

The Exa parser SHALL decode `POST /search` and `POST /contents` JSON responses within a bounded byte limit, extracting per result `url`, `id`, `title`, nullable `author` and `publishedDate`, `text`, `highlights`, `highlightScores`, `summary`, and outbound links (`extras.links`), plus `/contents` `statuses[]`. It SHALL reject malformed JSON, unknown fields, or unexpected wire shapes with an error.

#### Scenario: parse-exa-search-and-contents

- **WHEN** the parser decodes `servicesim` Exa `/search` and `/contents` responses
- **THEN** it returns the structured results, nullable metadata, outbound link lists, and `/contents` per-identifier statuses without error

#### Scenario: parse-exa-rejects-unexpected-shape

- **WHEN** a response body is malformed JSON, missing required top-level fields, or carries unknown fields
- **THEN** the parser returns an error

### Requirement: Split documents into passages with stable IDs and byte offsets

`Split` SHALL divide cleaned document content into ordered passages on structural boundaries (ported from `semsource`), assigning each passage a deterministic ID derived from document ID and ordinal and `[Start, End)` byte offsets that tile the input byte for byte without exceeding the hard maximum byte bound.

#### Scenario: split-tiles-input-with-stable-ids

- **WHEN** `Split` is called on a cleaned document
- **THEN** the returned passages tile `content` byte for byte (`concat(Body) == content`), each passage's `End - Start` is at most the hard max, and repeated calls produce identical passage IDs and spans

### Requirement: Build in-memory intake graph

`BuildGraph` SHALL construct an in-memory graph containing `question`, `provider`, `report`, `passage`, `source`, and `site` entities and `belongs_to` and `cites` edges derived from the parsed response structure, using local entity and triple types with no external transport dependency.

#### Scenario: graph-entities-and-edges

- **WHEN** `BuildGraph` runs on a parsed Exa response and question
- **THEN** the resulting graph contains the `question`, `provider`, `report`, `passage`, `source`, and `site` entities linked by `belongs_to` and `cites` triples

### Requirement: Run deterministic passage checks and score bands

`CheckAndScore` SHALL evaluate each passage against the eight named checks (`talks_to_reader`, `self_rank_claim`, `fake_markers`, `encoded_blob`, `unlisted_link`, `lone_site`, `shared_origin`, `off_topic`), compute a provisional weighted score and band (`low`, `middle`, `high`), and down-weight and report flagged passages without deleting any passage.

#### Scenario: checks-flag-and-band-without-deleting

- **WHEN** `CheckAndScore` evaluates a report containing clean passages and passages triggering deterministic checks
- **THEN** every input passage remains in the output, triggered checks are recorded by name on their passage, and each passage receives a `low`, `middle`, or `high` band

### Requirement: Parse Perplexity Agent API responses

`ParsePerplexityAgent` SHALL strictly decode `POST /v1/agent` JSON responses within a bounded byte limit, extracting `id`, `object`, `model`, `created_at`, `status`, `output[]` (`search_results` items with integer `id`, `title`, `url`, `snippet`, `date`, `last_updated`, `source` and `message` items with `output_text` text and `url_citation` `annotations`), optional `error`, and optional `usage`. It SHALL reject malformed JSON, unknown fields, non-integer search result IDs, or unexpected wire shapes with `ErrParseShape`.

#### Scenario: parse-perplexity-agent-response

- **WHEN** `ParsePerplexityAgent` decodes a `servicesim` Perplexity `POST /v1/agent` response
- **THEN** it returns the search results with integer IDs, message text, citation annotations, and usage metadata without error

#### Scenario: parse-perplexity-rejects-unexpected-shape

- **WHEN** a Perplexity response body is malformed JSON, carries a string search result `id`, is missing required fields, or contains unknown fields
- **THEN** `ParsePerplexityAgent` returns an error

### Requirement: Map Perplexity answer text to sources and run answer checks

The Perplexity intake path SHALL map answer sentences to `search_results` using `annotations` (`start_index`, `end_index`) when present and inline `[n]` markers when `annotations` are absent, and SHALL flag:
1. any inline `[n]` marker or annotation that points at no `search_results` entry (`dangling_citation_marker`),
2. any URL in the answer text that is not in `search_results` (`answer_unlisted_url`),
3. any substantive sentence in the answer with no resolved source (`unsourced_sentence`), and
4. any cited source whose page could not be fetched through Exa `/contents` (`unfetched_citation`).

#### Scenario: perplexity-answer-mapping-and-checks

- **WHEN** the Perplexity intake path evaluates responses with `annotations` or inline `[n]` markers, including dangling markers, unlisted answer URLs, unsourced sentences, and failed page fetches
- **THEN** it maps valid citations to `search_results` and records `dangling_citation_marker`, `answer_unlisted_url`, `unsourced_sentence`, and `unfetched_citation` findings accurately

### Requirement: Resolve cited Perplexity pages through Exa contents and run page checks

`ProcessPerplexityAgent` SHALL fetch the cited URLs through Exa `POST /contents`, run `Clean`, `Split`, `BuildGraph`, and the 8 page-level checks on the fetched pages, and report the count and share of cited pages that could not be fetched.

#### Scenario: perplexity-resolves-cited-pages-via-exa-contents

- **WHEN** `ProcessPerplexityAgent` runs against `servicesim`'s `perplexity` and `exa` profiles serving a shared scenario with clean, planted, and unresolvable cited URLs
- **THEN** it fetches the cited pages from Exa `/contents`, runs the 8 page-level checks on the fetched pages, and reports the unfetched citation count and share

