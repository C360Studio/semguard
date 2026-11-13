# Proposal: Perplexity Agent API intake path resolved through Exa contents

Addresses: #52
Track: design
Approved-by:

## Why

An Exa search result returns source page text directly, whereas a Perplexity Agent API response (`POST /v1/agent`) returns a synthesized model answer plus search result snippets and citation spans (`annotations` or inline `[n]` markers). Planted text in a cited page arrives digested in Perplexity's answer, so wording checks rarely fire on the answer itself; the signal is in the cited pages (fetched via Exa `POST /contents`) and in the answer-to-source mapping.

## What Changes

- **Perplexity Agent API parser (`intake/perplexity.go`):**
  - Strictly decode `POST /v1/agent` JSON responses (`id`, `object`, `model`, `created_at`, `status`, `output[]`, optional `error` and `usage`).
  - Extract `search_results` items (`queries`, `results[]` with integer `id`, `title`, `url`, `snippet`, `date`, `last_updated`, `source`) and `message` items (`id`, `role`, `status`, `content[]` with `output_text` text and `url_citation` `annotations[]` carrying `start_index`, `end_index`, `title`, `url`).
  - Reject malformed JSON, unknown fields, non-integer `results[].id`, or failed/missing required wire fields with `ErrParseShape`.
- **Answer-to-source mapping (`intake/perplexity.go`):**
  - Map answer sentences/spans to cited `search_results` using `annotations` (`start_index`, `end_index`) when present, and inline `[n]` markers (matching integer `results[].id`) when `annotations` are absent.
  - Record an explicit answer finding (`dangling_citation_marker`) when an inline `[n]` marker or annotation URL points to no entry in `search_results`.
- **Cited-page resolution via Exa `/contents` (`intake/perplexity.go`):**
  - Collect deduplicated cited URLs from the Perplexity response and fetch their page contents through an `ExaContentsFetcher` (backed by Exa `POST /contents` and `ParseExaContents`), then run `Clean`, `Split`, `BuildGraph`, and the 8 page-level checks from `#51` on the resolved pages.
  - Record per-citation fetch failures (from `/contents` `statuses[]` with `status != "success"` or missing results) and surface the unfetched citation count and share (`UnfetchedCount`, `CitedCount`, `UnfetchedShare`).
- **Answer-specific deterministic checks (`intake/perplexity.go`):**
  - `answer_unlisted_url`: a URL appearing in the answer text that is not in `search_results`.
  - `unsourced_sentence`: a substantive answer sentence that maps to no source (neither covered by an annotation span nor carrying a valid `[n]` citation).
  - `unfetched_citation`: a cited source whose page could not be fetched through Exa `/contents`.
  - `dangling_citation_marker`: an inline `[n]` marker or annotation referencing a non-existent search result.

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `intake`: add Perplexity `POST /v1/agent` response parsing, answer-to-source mapping (`annotations` and `[n]` fallback), cited-page resolution through Exa `POST /contents`, and the answer-specific deterministic checks, tested against `servicesim`'s `perplexity` and `exa` profiles together.

## Impact

- Extends `intake` (`intake/perplexity.go`, `intake/perplexity_test.go`, `intake/graph.go`); no changes to `go.mod` dependencies (`servicesim v0.6.0` is already present).
