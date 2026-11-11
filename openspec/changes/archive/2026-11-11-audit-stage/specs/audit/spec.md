# Audit

## ADDED Requirements

### Requirement: Document-blind canonical audit input

`FormatInput` SHALL serialize only the trusted `untrusted.Task` (`Instruction` and `Question`) and the validated `verify.Record` (`Scores` and `Claims`) without accepting or embedding any source `Document` text.

#### Scenario: audit-format-excludes-documents

- **WHEN** `FormatInput` formats a task and a `verify.Record`
- **THEN** the resulting string contains the task instruction, question, scores, and claims/quotes from the record and never receives or includes unquoted source document paragraphs

### Requirement: Decision model audit scorer via semselect

`SemselectAuditor.Audit` SHALL score a `(Task, verify.Record)` pair by sending bounded Noul (`yes`/`no`) probes ("is this record on task" and "does any claim contradict the question") to `POST /v1/decisions` on a running `semselect` service with context deadlines and bounded request/response bodies, returning `LabelFlag` when the combined off-task/contradiction probability meets or exceeds `Threshold` (default `0.5`) and `LabelPass` otherwise.

#### Scenario: audit-semselect-on-task-and-contradicts

- **WHEN** `SemselectAuditor.Audit` runs against a `semselect` `/v1/decisions` endpoint for an on-task record and an off-task or contradictory record
- **THEN** it returns `LabelPass` (`Score < 0.5`) on the clean record, `LabelFlag` (`Score >= 0.5`) with the triggering reason (`off_task` or `contradicts`) on the hijacked record, and fails on transport or HTTP errors

### Requirement: Small chat model audit scorer

`ChatAuditor.Audit` SHALL score a `(Task, verify.Record)` pair through `internal/reader.Client` in `ModeSchema` constrained to a JSON label (`pass` or `flag`) and reason (`on_task`, `off_task`, or `contradicts`), returning `LabelFlag` (`Score: 1.0`) or `LabelPass` (`Score: 0.0`).

#### Scenario: audit-chat-constrained-label

- **WHEN** `ChatAuditor.Audit` runs against an OpenAI-compatible chat endpoint in `ModeSchema`
- **THEN** it sends a strict JSON schema constraining the output to `label` and `reason` and maps `flag` to `LabelFlag` (`Score: 1.0`) and `pass` to `LabelPass` (`Score: 0.0`)
