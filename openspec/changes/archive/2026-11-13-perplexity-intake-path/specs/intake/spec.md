## ADDED Requirements

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
