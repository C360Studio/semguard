# Tasks

- [ ] 1. Owner approves `proposal.md` and `design.md` and rules on Open Questions 1, 2, and 3.
  Hold: OPEN-Q proposal awaits owner approval
- [ ] 2. `eval/schemas/score-compare.json` and `internal/verify` (`Document`, `Record`, `Violation`, `Verify`, `ParseFree`, `AllowedLinks`). Premise: design Decisions 1, 3, 4, 5; `internal/corpus/corpus.go:100` (1-based `\n\n` paragraph split) and `:189` (`urlRe`). Check: unit tests citing `verify-*` scenarios, a `testing.F` fuzz target on `Verify` / `AllowedLinks` (datamark `^` normalization and URL extraction), and a non-destructive mutation check (`task mutate:check`).
- [ ] 3. `internal/reader` (`Client`, `Request`, `Response`, `ModeFree`, `ModeSchema`, `Complete`). Premise: design Decision 2; `AGENTS.md:27,39-41`. Check: `httptest.Server` unit tests citing `reader-*` scenarios (free mode, schema mode with `SchemaSHA256`, refusal B2, bounded response body, ctx cancellation), plus a non-destructive mutation check (`task mutate:check`).
- [ ] 4. `task verify` green, then archive this change into `openspec/specs/reader/` and `openspec/specs/verify/` before the PR is marked ready.
