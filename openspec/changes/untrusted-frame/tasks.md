# Tasks

- [x] 1. Owner approves proposal.md and design.md (Open Questions 1 and 2 were ruled on 2026-11-11).
- [ ] 2. `internal/untrusted`: `Document`, `Task`, `Datamark`, `Unmark`. Premise: design Decisions 1, 2, 4, 6;
  `internal/corpus/corpus.go:100` (paragraph split). Check: one test per `datamark-*` scenario citing it, plus a
  `testing.F` fuzz of the round-trip law `Unmark(paragraph(Datamark(d))) == paragraph(d)` over single-spaced text,
  shown able to fail by a non-destructive mutation (`cp` + SHA-256).
- [ ] 3. `Frame`, `Options{Datamark, Framing bool; MaxBytes int}`, `Prompt`, and the embedded `prompts/frame.txt`. Premise:
  Decisions 3 and 5; `corpus/tasks.json` (instruction). Check: one test per `frame-*` scenario, covering all four switch cells, the pinned
  `FramingSHA256`, and a test that a loaded corpus case under the default cap frames without error.
- [ ] 4. `task verify` green, then archive this change into `openspec/specs/untrusted/` before the PR is marked ready.
