# Design: Exa intake path and task-independent verification checks

Architect: `gemini (pro, low)`

## Context

- **Pre-check 1 (`servicesim` text pass-through):** Verified in `/Users/coby/Code/c360/servicesim` (`1a7386a83533`, `v0.6.0`). `scenario/render.go:28-51` (`scenario.Render`) copies `src.Text` verbatim (`line 37`). `profiles/exa/render.go:364-381` (`renderResult`) sets `Text: cmp.Or(rp.Text, src.Text)`. `internal/wire/render.go:93-101` (`marshal`) uses `json.NewEncoder` with `SetEscapeHTML(false)`. Valid UTF-8 invisible runes (`Cf` such as `U+200B`, `Cc`, HTML tags, `^`, `<<report`) survive `servicesim` unchanged; invalid raw UTF-8 bytes cannot be authored in YAML and are replaced with `U+FFFD` by `encoding/json`, so invalid-UTF-8 rejection in `Clean` needs direct byte fixtures alongside `servicesim` tests.
- **Pre-check 2 (`semengine` graph/message imports):** Verified in `/Users/coby/Code/c360/semengine` (`ee402b4`). While `semengine/graph` (`graph/graphable.go:54-60`, `graph/types.go:23-43`) and `semengine/message` (`message/triple.go:37-100`) do not import `nats.go` directly in `go list -f '{{.Deps}}'`, `semengine/go.mod:11-31` is a single root module requiring `nats-io/nats-server/v2`, `nats-io/nats.go`, `moby/moby`, `testcontainers-go`, `prometheus`, and `go 1.26.11` (above `semguard`'s `go 1.26.4` in `go.mod:3`). Per `#51` ("If they do pull one in, define local types of the same shape and say so in the design"), `semguard` defines local `EntityState`, `Triple`, and `Graphable` types of the same shape and adds only `github.com/c360studio/servicesim v0.6.0` to `go.mod`.
- **Pre-check 3 (`semsource` splitter tests):** Verified in `/Users/coby/Code/c360/semsource/handler/doc/splitter.go:1-715` and `splitter_test.go:1-460` (`4ca2072c5d9d`). `splitter.go` imports only `"strings"`, `"unicode"`, and `"unicode/utf8"`. One test in `splitter_test.go:57` (`TestPassagesTitle_AncestryAndCollapse`) calls `passageTitle` from `entities.go:155-169`, and `TestSplitPassages_TilesRealCorpus` (`splitter_test.go:25-54`) asserts `checked >= 50` `.md` files (whereas `semguard` has ~25 `.md` files unless `corpus/cases.json` documents or a lower threshold are included). All other splitter tests lift with zero changes.
- **Existing `semguard` seams:** `untrusted/untrusted.go:20-24,47-86,106-283` defines `Document`, `Paragraph`, `OffsetMap`, `Segment`, `Strip`, `Datamark`, `Unmark`, and `Frame`. `verify/verify.go:30-65,103-194,287-398` defines `Document`, `Claim`, `Record`, `Violation`, `Verify`, `verifyClaim`, `AllowedLinks`, and `urlRe`. `Taskfile.yml:22` runs `gofmt -l cmd internal untrusted verify`.

## Goals / Non-Goals

Goals: build the deterministic Exa intake pipeline (`clean -> parse -> split -> graph -> checks -> score`), test it offline against `servicesim`'s Exa profile (`testkit`), and fold in `#31` (`verify` quote-in-passage check, unified `Document`, `Strip`+`Frame` example). Non-goals: Perplexity (`#52`), corpus measurement (`#53`), decision model (`#54`), live network calls (`#55`), or importing `semengine`.

## Decisions

1. **Package layout for the intake pipeline (`#51`, `README.md:58-70`, `Taskfile.yml:22`).**
   - **(a) Single exported package `intake` (`github.com/c360studio/semguard/intake`):** Holds `Clean`, Exa parser (`ParseExaSearch`, `ParseExaContents`), ported splitter (`Split`, `Passage`), in-memory `Graph` (`EntityState`, `Triple`), the 8 checks, and `Score`/`Band`, with Perplexity (`#52`) joining `intake` next.
   - **(b) Core `intake` package plus provider subpackage `intake/exa`:** `intake` holds provider-neutral `Clean`, `Split`, `Graph`, `Checks`, and `Score`; `intake/exa` holds Exa wire decoding and conversion into `intake.Report`.
   - *Trade-off:* Option (a) is the smallest flat footprint (one new directory, one import for consumers and `#53`) and avoids cross-package type plumbing. Option (b) isolates Exa and Perplexity (`#52`) wire structs in subpackages at the cost of extra packages and exports.
   - *Adopter seam:* Under (a), callers import `github.com/c360studio/semguard/intake`; under (b), callers also import `github.com/c360studio/semguard/intake/exa`. If a developer does nothing, `Taskfile.yml:22` (`gofmt -l`) must include `intake` either way.

2. **`Clean` HTML/active-markup handling and URL canonicalization (`#51` step 1, `untrusted/untrusted.go:130-180`).**
   - **(a) Reject non-UTF-8 or oversized input (`MaxInputBytes = 1 << 20`), strip `<script>...</script>`, `<style>...</style>`, HTML comments, active tags (`<iframe>`, `<object>`, `<embed>`, `<svg>`, `<form>`, `<meta>`, `<link>`), inline `on*=` attributes, and `javascript:`/`data:`/`vbscript:` URLs while preserving Markdown structure and prose, canonicalize every `http://`/`https://` URL in text (`net/url`: lowercase scheme and host, strip `:80`/`:443` and `#fragment`, clean path), and run `untrusted.Strip`:** Returns `(Cleaned, error)` with the `untrusted.OffsetMap`.
   - **(b) Strip all `<...>` HTML tags unconditionally before `untrusted.Strip` and canonicalize URLs.**
   - *Trade-off:* Option (a) removes active markup without corrupting mathematical `<` comparisons or Markdown autolinks, whereas (b) can eat prose between `<` and `>`.
   - *Adopter seam:* `Clean(ctx, raw, opts)` rejects invalid UTF-8 and inputs over `MaxBytes` with an error; callers pass `Cleaned.Text` to `Split` and `untrusted.Frame` without hitting marker-spoofing errors.

3. **Exa outbound link list representation in `Parse` and `servicesim` (`#51` step 2 & check 5, `servicesim/profiles/exa/response.go:35-69`).**
   - **(a) Parse `results[].extras.links` (`[]string`, Exa's OpenAPI response shape documented in `servicesim/profiles/exa/contracts/README.md:813`) and also accept top-level `results[].links` (`[]string`) via `servicesim` `extra_fields`:** When `extras` or `links` is present (`LinksPresent == true`, even if empty), check 5 (`unlisted_link`) flags any URL in the passage text whose canonical form is not in the result's canonicalized link list; when neither field was returned by the route (`LinksPresent == false`), `unlisted_link` does not fire.
   - **(b) Parse only `results[].extras.links` and require every `servicesim` test to set `extra_fields: {extras: {links: [...]}}`.**
   - *Trade-off:* Option (a) matches the live Exa OpenAPI contract (`results[].extras.links`) for `#55` while allowing concise `extra_fields` in scenarios; Option (b) is stricter to OpenAPI only.
   - *Adopter seam:* `Result.LinksPresent` distinguishes "provider returned an empty outbound link list" (every URL in text is unlisted) from "provider did not include link metadata on this call".

4. **Check 8 (`off_topic`) and check 7 (`shared_origin`) deterministic rules (`#51` step 5).**
   - **(a) Content-word overlap and normalized sentence/shingle sharing:**
     - `off_topic`: extract lowercase alphanumeric content tokens (length ≥ 3, excluding a small fixed English stopword set) from `question` and from the passage's sibling passages in the same `report`; flag a passage (≥ 10 content tokens) when it shares zero content tokens with `question` AND zero content tokens with the rest of its `report` (or < 5% Jaccard when the report has ≥ 2 passages).
     - `shared_origin`: flag a passage when two results on different `site` hosts share an identical normalized sentence (≥ 60 chars) or 8-word shingle, or share the same canonical non-site origin metadata (identical non-empty `author` + `publishedDate` millisecond timestamp across distinct sites).
   - **(b) Require `off_topic` to fire whenever overlap with `question` alone is zero, regardless of overlap with the rest of the report.**
   - *Trade-off:* Option (a) implements `#51`'s exact wording ("off-topic for the question and for its own report (keyword overlap; no model)") so background context sections that connect to the report are not false-flagged; Option (b) is simpler but over-flags background passages in multi-section pages.
   - *Adopter seam:* All 8 check IDs and provisional weights/band cut-offs (`Low < 2`, `Middle 2..4`, `High >= 5`) are exported constants so `#53` can inspect per-check and per-band counts directly.

5. **Folding in `#31`: `Document` unification and task-independent quote check (`verify/verify.go:30-34,287-360`, `untrusted/untrusted.go:20-24`).**
   - **(a) Type alias `type Document = untrusted.Document` in `verify` (adding `json:"id"` and `json:"text"` tags to `untrusted.Document`), plus exported `verify.QuoteInPassage(quote, quoteSHA256, passageText string) error` and `verify.VerifyClaim(docID string, paragraph int, quote, quoteSHA256 string, docs []Document) error`:** `verify.Verify` calls `VerifyClaim` internally; existing callers of `untrusted.Document` and `verify.Document` compile unchanged without conversions.
   - **(b) Keep `untrusted.Document` and `verify.Document` as distinct types and document why in package doc comments.**
   - *Trade-off:* Option (a) eliminates `Document` conversion boilerplate for every consumer while keeping full backward compatibility (`verify.Document` remains a valid type name with the same JSON tags). Option (b) avoids `verify` importing `untrusted` (an internal sibling package in the same module).
   - *Adopter seam:* A caller with its own reader output struct calls `verify.VerifyClaim` (for `\n\n` paragraphs) or `verify.QuoteInPassage` (for any `intake.Passage.Body`) and `verify.AllowedLinks` directly.

## Risks / Trade-offs

- Adding `github.com/c360studio/servicesim v0.6.0` to `go.mod` adds test-time indirect dependencies (`go-cmp`, `testify`, `yaml.v3`) while keeping `semguard`'s non-test production packages stdlib-only.
- Provisional check weights and regex/token heuristics in `#51` may under- or over-flag on `corpus/cases.json` until `#53` measures and tunes them once under the stop rule.

## Migration Plan

1. After owner approval, add `servicesim v0.6.0` (via local `replace` or module proxy if published), implement `#31` in `untrusted` and `verify`, and implement `intake` with `servicesim` and ported `semsource` splitter tests.
2. Archive `openspec/changes/exa-intake-path/` into `openspec/specs/{intake,untrusted,verify}/spec.md` before marking the PR ready.

## Open Questions

1. **(owner) Package layout (`Decision 1`):** Single exported package `github.com/c360studio/semguard/intake` (Option a) or split `intake` + `intake/exa` (Option b)?
2. **(owner) `Clean` active-markup removal (`Decision 2`):** Targeted removal of script/style/active tags, `on*=` attributes, and dangerous URL schemes while preserving `<` in prose/Markdown (Option a), or strip all `<...>` tags (Option b)?
3. **(owner) Exa outbound links and `off_topic`/`shared_origin` rules (`Decisions 3 & 4`):** Confirm Option (a) for `results[].extras.links` (with `LinksPresent` gating `unlisted_link`) and Option (a) for `off_topic` + `shared_origin` heuristics and provisional band cut-offs?
4. **(owner) `#31` `Document` unification (`Decision 5`):** Alias `verify.Document = untrusted.Document` and export `VerifyClaim` + `QuoteInPassage` (Option a), or keep two `Document` structs (Option b)?
