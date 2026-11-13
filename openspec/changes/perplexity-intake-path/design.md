# Design: Perplexity Agent API intake path resolved through Exa contents

Architect: `gemini (pro, low)`

## Context

- **`servicesim` Perplexity Agent API wire shape (`servicesim@v0.6.0/profiles/perplexity/response.go:220-384`, `agent.go:195-256`):** `POST /v1/agent` (and alias `/v1/responses`) returns `responsesResponse` (`id`, `object: "response"`, `model`, `created_at`, `status`, `output[]`, optional `error`, optional `usage`). `output[]` is an ordered trace with two item types:
  - `type: "search_results"` (`queries []string`, `results []agentSearchResult` where `id` is a 1-based integer `int`, `title`, `url`, `snippet`, optional `date`, `last_updated`, `source: "web"`).
  - `type: "message"` (`id`, `role: "assistant"`, `status`, `content []contentPart` where each `type: "output_text"` part carries `text string` and `annotations []annotation` with `type: "url_citation"`, byte offsets `start_index`, `end_index`, `title`, `url`).
- **Existing `semguard/intake` seams (`intake/clean.go:73-227`, `intake/exa.go:47-253`, `intake/graph.go:61-255`, `intake/checks.go:10-49`, `intake/score.go:84-255`):**
  - `ParseExaContents(ctx, body, opts)` decodes Exa `POST /contents` (`results[]` and `statuses[]` with per-URL `status: "success" | "error"`).
  - `PrepareExaReports(results, cleanOpts)` and `CheckAndScore(question, providerName, reports, scoreOpts)` run `Clean`, `Split`, `BuildGraph`, and the 8 page-level checks (`talks_to_reader` .. `off_topic`).

## Goals / Non-Goals

Goals: parse Perplexity `POST /v1/agent` responses strictly, map answer sentences/spans to `search_results` via `annotations` or inline `[n]` markers, resolve cited page URLs through Exa `POST /contents`, run the 8 page-level checks on resolved pages, and run answer-specific checks (`answer_unlisted_url`, `unsourced_sentence`, `unfetched_citation`, `dangling_citation_marker`), tested against `servicesim`'s `perplexity` and `exa` profiles in one scenario. Non-goals: retired Sonar (`POST /v1/sonar`), SSE streaming, or background run polling (`GET /v1/agent/{id}`).

## Decisions

1. **Cited-page fetching seam between Perplexity and Exa `POST /contents` (`#52` step 3, `intake/exa.go:207-253`).**
   - **(a) Provide both a pure slice/callback seam `ExaContentsFetcher func(ctx context.Context, urls []string) ([]byte, error)` (with an HTTP helper `NewHTTPExaContentsFetcher(client *http.Client, baseURL, apiKey string) ExaContentsFetcher`) AND a direct pre-fetched `ResolvePerplexityWithContents(question string, pplx PerplexityResponse, contents ExaContentsResponse, cleanOpts CleanOptions, scoreOpts ScoreOptions) (PerplexityIntakeResult, error)`:** Callers with an HTTP endpoint (such as `servicesim` or `#55`) pass `NewHTTPExaContentsFetcher(sim.Client(), sim.URL(exa.Name), key)`, while pure unit tests or offline callers can pass an already-parsed `ExaContentsResponse`.
   - **(b) Require an `*http.Client` and Exa base URL on every Perplexity intake call (`ProcessPerplexityAgent(ctx, question, body, httpClient, exaURL, ...)`).**
   - *Trade-off:* Option (a) keeps the core intake logic pure and testable with or without HTTP while making the two-provider `servicesim` test a one-liner; Option (b) couples every test to `net/http`.
   - *Adopter seam:* Under (a), `ProcessPerplexityAgent(ctx, question, body, fetcher, ...)` invokes `fetcher(ctx, citedURLs)` only when `len(citedURLs) > 0` and delegates to `ResolvePerplexityWithContents`. If a developer passes a `nil` fetcher when citations exist, it marks all cited URLs as unfetched (`unfetched_citation`).

2. **Answer-to-source mapping and `unsourced_sentence` / `dangling_citation_marker` rules (`#52` steps 2 & 4).**
   - **(a) Sentence-level mapping with `annotations` primary and `[n]` fallback:**
     - Split the cleaned answer text into sentences (preserving `[Start, End)` byte spans).
     - When `annotations` is non-empty on the message content part: validate each `annotation`'s `[start_index, end_index)` against the raw answer text, match its canonical `url` to `search_results` (recording `dangling_citation_marker` if `url` is absent from `search_results` or out of bounds), and attribute an answer sentence to every `annotation` whose span overlaps or immediately trails the sentence. Also scan for inline `[n]` markers: if `[n]` points to no `search_results[].id`, record `dangling_citation_marker`.
     - When `annotations` is empty: attribute each sentence using inline `[n]` markers (e.g. `[1]`, `[2]`) matched against integer `search_results[].id`, recording `dangling_citation_marker` for any `[n]` whose integer `n` is not in `search_results`.
     - Any non-heading sentence with ≥ 5 content words that resolves to zero valid sources triggers `unsourced_sentence`.
   - **(b) Exclusive mode switch (ignore inline `[n]` markers completely whenever `len(annotations) > 0`, and flag every sentence without an overlapping annotation even if it carries `[1]`).**
   - *Trade-off:* Option (a) follows `#52` ("from `annotations` (start and end offsets) when present, and from inline `[n]` markers when not. A marker that points at no result is a finding") while catching dangling `[99]` markers in either mode; Option (b) misses dangling `[n]` markers when `annotations` is non-empty.
   - *Adopter seam:* `PerplexityIntakeResult.SentenceCitations` records each sentence's byte span and resolved `SearchResultIDs` / `URLs`, and `AnswerFindings` lists every answer-level finding.

3. **Which URLs are fetched via Exa `/contents` and how `unfetched_citation` is counted (`#52` steps 3 & 4).**
   - **(a) Fetch all unique canonical URLs cited by the answer (from `annotations` and valid `[n]` references, falling back to all `search_results[].url` when the answer cites any result or when `FetchAllSearchResults` is set; default: union of `search_results[].url` and cited `annotations[].url` that appear in `search_results`):** Specifically, every URL in `search_results` that is cited by the answer (or all `search_results` URLs when cited) is requested from Exa `POST /contents` in deterministic order. A cited URL is marked unfetched (`unfetched_citation`) if `/contents` returns `status != "success"` for it in `statuses[]` or omits its result body (`text == ""`). `PerplexityIntakeResult` reports `CitedURLs`, `FetchedCount`, `UnfetchedCount`, and `UnfetchedShare` (`float64(UnfetchedCount)/float64(len(CitedURLs))`).
   - **(b) Fetch only URLs appearing in `annotations[].url` and ignore `search_results` cited via `[n]`.**
   - *Trade-off:* Option (a) ensures cited pages are fetched whether the response used `annotations` or `[n]` markers, and computes the exact unfetched share `#52` requires; Option (b) fails on `[n]`-only responses.
   - *Adopter seam:* `PerplexityIntakeResult.PageIntake` holds the `intake.Result` from running `CheckAndScore` on the successfully fetched Exa pages (plus answer-to-source edges in `Graph`).

## Risks / Trade-offs

- As `#52` notes, when Perplexity's cited URLs do not overlap across multiple independent reports, `lone_site` on the fetched pages may fire frequently; `#55` will observe real overlap rates.
- Byte offsets in `annotations` (`start_index`, `end_index`) refer to the raw `content[].text` before `Clean`; mapping them before `Clean` (or via `Cleaned.Offsets`) avoids offset drift when invisible characters are stripped.

## Migration Plan

1. After owner approval of `proposal.md` and `design.md`, implement `intake/perplexity.go` and `intake/perplexity_test.go`, archive `perplexity-intake-path` into `openspec/specs/intake/spec.md`, run `task verify` and `scripts/ateam bench --selftest`, and request cross-agent review.

## Open Questions

1. **(owner) Cited-page fetching seam (`Decision 1`):** Approve Option (a) (`ExaContentsFetcher` callback + `NewHTTPExaContentsFetcher` + `ResolvePerplexityWithContents`), or Option (b) (HTTP-only entry point)?
2. **(owner) Answer-to-source mapping and `unsourced_sentence` / `dangling_citation_marker` rules (`Decision 2`):** Approve Option (a) (`annotations` primary + `[n]` fallback, flagging any `[n]` or annotation pointing to no `search_results` entry, and flagging sentences with ≥ 5 content words and no valid source), or Option (b)?
3. **(owner) Cited-page set and `unfetched_citation` reporting (`Decision 3`):** Approve Option (a) (fetch deduplicated cited `search_results` URLs in order through Exa `/contents`, flag any cited URL with `status != "success"` or empty text as `unfetched_citation`, and report `CitedURLs`, `FetchedCount`, `UnfetchedCount`, `UnfetchedShare`), or Option (b)?
