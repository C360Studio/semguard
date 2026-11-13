## ADDED Requirements

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
