# Exa intake path and task-independent verification checks

Approved-by:

Track: design (adds `openspec/specs/intake/spec.md`, updates `openspec/specs/verify/spec.md` and `openspec/specs/untrusted/spec.md`, adds exported intake packages, folds in #31, and adds `servicesim` as a test dependency).

## Why

1. **Pre-reader intake checks (#51):** Milestone 1 showed that basic injections mostly fail on large reader models, framing text removes most of a score-nudging effect on one model, and a document-blind auditor does not earn its cost—leaving quiet score/judgment manipulation (`rank`) as the one attack that worked and that no output check can see. Milestone 4 moves the question earlier: before any model reads an incoming research report, can deterministic code checks (and later a fast decision model) make planted text stand out?
2. **Task-independent verification and document seam (#31):** `verify.Verify` is coupled to the `score-compare` wire schema, keeping the paragraph quote check (`verifyClaim`) unexported (`verify/verify.go:287`), `untrusted.Document` (`untrusted/untrusted.go:21`) and `verify.Document` (`verify/verify.go:31`) are duplicate structs, and the `Strip` → `Frame` sequence lacks an executable example.

## What Changes

- **Intake package(s) (`intake` / `intake/exa`):**
  1. **Clean:** Enforce context and input byte cap, validate UTF-8, strip invisible/format (`Cf`) and control (`Cc` except `\n`, `\t`) runes and reserved markers (reusing `untrusted.Strip`), remove active HTML/script/style/event-handler markup, and canonicalize URLs (lowercase scheme/host, strip default ports and fragments, clean paths). Reject oversized or malformed inputs.
  2. **Parse (Exa):** Strict decoding of Exa `POST /search` (with contents and optional `output.grounding`) and `POST /contents` responses: per result `url`, `id`, `title`, nullable `author` and `publishedDate`, `text`, `highlights`, `highlightScores`, `summary`, and `extras.links` (outbound link list when present), plus `/contents` `statuses[]`. Reject unexpected shapes or unknown fields.
  3. **Split:** Port `semsource`'s structural passage splitter (`handler/doc/splitter.go`, MIT, `4ca2072c5d9d`) with its tests and deterministic passage IDs (`<doc-id>#p<ordinal>`) and `[Start, End)` byte offsets that tile the cleaned text byte for byte.
  4. **Graph:** In-memory entities (`question`, `provider`, `report`, `passage`, `source`, `site`) and edges (`belongs_to`, `cites`) built from the parsed response structure using local types matching `semengine`'s `EntityState`/`Triple` shape (zero transport/NATS dependencies).
  5. **Checks & Score:** Eight deterministic checks returning named findings per passage (`talks_to_reader`, `self_rank_claim`, `fake_markers`, `encoded_blob`, `unlisted_link`, `lone_site`, `shared_origin`, `off_topic`), plus a provisional weighted score and band (`low`, `middle`, `high`) that down-weights and reports flagged passages without deleting them.
- **Task-independent `verify` & `untrusted` (#31):** Export quote-in-passage/paragraph verification in `verify`, unify or alias `Document` across `untrusted` and `verify`, and add a doc comment and `ExampleStrip_andFrame` in `untrusted`.
- **Tests:** In-process `servicesim` (`v0.6.0`, `profiles/exa` + `testkit`) integration tests plus direct cleaning fixtures and `testing.F` / mutation checks.

## Non-goals

- Perplexity Agent API path (#52), corpus-wide measurement (#53), decision-model escalation (#54), live smoke calls (#55), or any NATS/graph-engine dependency.

## Impact

- Added: `openspec/specs/intake/spec.md`, `intake/` (and/or `intake/exa/`).
- Modified: `go.mod`, `go.sum`, `Taskfile.yml` (`gofmt` list), `README.md`, `untrusted/`, `verify/`, `openspec/specs/{untrusted,verify}/spec.md`.

## Done when

Owner resolves Open Questions 1–4 and approves this proposal; `intake` and #31 changes pass `task verify` (including `servicesim` in-process Exa tests, ported `semsource` splitter tests, `testing.F` fuzzing, and `task mutate:check`).
