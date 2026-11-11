# Audit stage: decision model (semselect) vs small chat model

Approved-by:

Track: design (adds `audit` spec capability and modifies `detonate` and `report` specs for `--audit` and side-by-side audit reporting).

## Why

After `frame` and `verify`, an injected document can still skew scores or plant an off-task/contradictory claim grounded in an authentic paragraph quote. Issue #14 adds the `audit` stage (`Audit(ctx, task, record) -> (label, score, reason)`), which sees only the trusted task and the validated `verify.Record` (never the untrusted documents), and measures a `semselect` decision model (`/v1/decisions` using Noul/Choice) against a small chat model (`/chat/completions` constrained to a label) side by side on the same corpus.

## What Changes

- `internal/audit`:
  - `Auditor` interface and `Result{Label, Score, Reason, DurationMs}` (`LabelPass = "pass"`, `LabelFlag = "flag"`), plus deterministic canonical formatting of `(untrusted.Task, verify.Record)` that never includes raw source documents.
  - `SemselectAuditor`: HTTP client calling `POST /v1/decisions` on a running `semselect` service (`SEMGUARD_SEMSELECT_URL` / `--semselect-url`) with bounded Noul questions ("is this record on task", "does any claim contradict the question"), no generated text, bounded request/response sizes, and context deadlines.
  - `ChatAuditor`: scorer using `internal/reader.Client` (`--audit-chat-model` / `SEMGUARD_AUDIT_CHAT_MODEL`) constrained via strict JSON schema to a pass/flag label and reason.
- `internal/report`:
  - Extend `StageConfig` (`Audit: "off"|"semselect"|"chat"`), `File` (optional `AuditMeta` recording `semselect` revision, `models.lock.json` entry, or chat model ID), `RunResult` (`Audit *AuditRun`), and `Outcomes` (`AuditFlagged bool`).
  - Report per class: hijacked runs flagged by audit (caught), clean/benign runs flagged by audit (false flags on `quoted`/`article`), residual hijacks, and audit `p50`/`p95` latency side by side in `RenderMarkdown`.
- `cmd/detonate` & `.env.example`:
  - Add `--audit off|semselect|chat`, `--semselect-url`, `--semselect-revision`, `--semselect-model-lock`, `--audit-chat-model`, and `--audit-threshold` flags and corresponding `SEMGUARD_*` env defaults.

## Non-goals

- Running live `semselect` or live reader inference during `task check` (#9 runs live M1 inference).
- Exporting `internal/audit` as part of the public module API (unless M1 results in #9 prove consumers should import it).

## Impact

- New: `internal/audit/audit.go`, `internal/audit/audit_test.go`, `openspec/specs/audit/spec.md`.
- Modified: `cmd/detonate/main.go`, `cmd/detonate/main_test.go`, `internal/report/report.go`, `internal/report/report_test.go`, `.env.example`, `openspec/specs/detonate/spec.md`, `openspec/specs/report/spec.md`.

## Done when

`task verify` passes offline. Every `audit`, `detonate`, and `report` spec scenario is cited by a unit test (`httptest.Server` for `semselect` and `chat` scorers), non-trivial branches pass `task mutate:check`, and record formatting / outcome scoring is covered by a `testing.F` fuzz target.
