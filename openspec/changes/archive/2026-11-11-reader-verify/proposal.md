# Reader client, per-task schema, and verify

Approved-by: Coby (2026-11-11) sha256:81f9abe76dd7

Track: design (adds `reader` and `verify` specs and the surface milestone 3 exports, #7).

## Why

The reader is the only box that writes text, and `verify` is the code box that constrains what consumers can trust from it. Before `cmd/detonate` (#8) and the audit comparison (#14) can run, semguard needs the one OpenAI-compatible chat client (`internal/reader`) in `free` and `schema` modes, the `score-compare` JSON schema (`eval/schemas/score-compare.json`), and the `internal/verify` validator that enforces schema adherence, claim resolution (`paragraph` + `quote_sha256`), and URL allowlisting (H2).

## What Changes

- `eval/schemas/score-compare.json`: strict JSON Schema for the `score-compare` task (`scores` 0–10 per document, `claims` with `doc_id`, `paragraph`, `text`, `quote`, and `quote_sha256`, plus overall `confidence`).
- `internal/reader`: one OpenAI-compatible `/v1/chat/completions` client (`Complete(ctx, req) (Response, error)`), ctx-honoring, bounded request/response bytes, no retries, no tools. Supports `ModeFree` (plain completion) and `ModeSchema` (`response_format: json_schema` strict mode). Records the raw response, refusal when declined (B2), and SHA-256 of the schema bytes sent.
- `internal/verify`: `Verify(raw, docs, opts) (Record, []Violation)` and `AllowedLinks(output, docs) []Violation` with its own `Document{ID, Text}` type (exported in #7). Validates against the task schema, resolves every claim's `doc_id` and 1-based `paragraph` index against `\n\n`-split paragraphs, verifies `quote_sha256` against paragraph bytes (normalizing datamark `^` when present), and flags any URL absent byte-for-byte from every document (H2). Rejects on any violation (B1 in `schema` mode).

## Non-goals

- No retries, provider SDKs, streaming, or tool definitions in `internal/reader`.
- No prompt framing (`internal/untrusted`, #5), runner (`cmd/detonate`, #8), or audit stage (#14).
- No live inference in `task check` / `task verify`; tests use `httptest.Server` and synthetic fixtures.

## Impact

- New: `eval/schemas/score-compare.json`, `internal/reader/`, `internal/verify/`, `openspec/specs/reader/spec.md`, `openspec/specs/verify/spec.md`.
- Coordinates with #5 (`internal/untrusted`) on `Prompt{System, User}` message split and `^` datamark quote normalization.

## Done when

`task verify` passes offline. Every `reader` and `verify` spec scenario is cited by a unit test, non-trivial branches pass `task mutate:check`, and claim/link verification is covered by `testing.F` fuzzing.
