# Tasks

- [x] 1. Owner approves `proposal.md` and `design.md` and rules on Open Questions 1, 2, and 3.
- [x] 2. Promote `internal/verify` → `verify` and `internal/untrusted` → `untrusted`, updating imports in `cmd/detonate/main.go`, `internal/report/report.go`, `Taskfile.yml`, and `README.md`. Premise: design Decision 3; `Taskfile.yml:22`; `README.md:51-52`. Check: `go test -race ./...` and `task lint` pass across top-level `untrusted` and `verify`.
- [x] 3. Implement `Segment(text) []Paragraph`, `Strip(text) (string, OffsetMap)`, and `Block` in `untrusted`. Premise: design Decisions 1, 2, 3; `internal/corpus/corpus.go:100` (`\n\n` split); `untrusted/untrusted.go:59-69` (`check` marker rules). Check: unit tests citing `segment-*` and `strip-*` scenarios, a `testing.F` fuzz target proving `Segment` parity with `corpus.Paragraphs`, `Strip` idempotence + `Datamark` safety + `OffsetMap` byte-mapping invariant, and a non-destructive mutation check (`task mutate:check`).
- [x] 4. `task verify` green, then archive this change into `openspec/specs/untrusted/` before the PR is marked ready.
