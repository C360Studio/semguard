# Tasks: audit-stage

- [x] 1. Resolve Open Questions 1–2 in `openspec/changes/audit-stage/design.md` and record `Approved-by: Coby (2026-11-11) sha256:dd996164579e` in `openspec/changes/audit-stage/proposal.md`.
- [x] 2. Implement `internal/audit` (`FormatInput`, `SemselectAuditor`, `ChatAuditor`, `Result`) grounded in `verify/verify.go:37-56` and `internal/reader/reader.go:36-62,120-249`, with unit tests (`TestAuditFormatExcludesDocuments`, `TestSemselectAuditor`, `TestChatAuditor`), `FuzzAuditFormatAndScore`, and mutation check (`scripts/ateam mutate`).
- [x] 3. Extend `internal/report` (`internal/report/report.go:29-99,189-310,400-506`) and `cmd/detonate` (`cmd/detonate/main.go:41-55,217-233,295-350`) with `--audit off|semselect|chat`, `semselect` revision/lockfile metadata, audit outcome tallies (`AuditFlagged`, caught hijacks, false flags on `quoted`/`article`), and side-by-side audit comparison rendering in `RenderMarkdown`, verified by `task verify`.
