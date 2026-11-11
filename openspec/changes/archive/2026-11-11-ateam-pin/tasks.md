# Tasks

- [x] 1. Owner approves proposal.md and design.md.
- [x] 2. `.agents/ateam.json` pin; CI and merge-check install lines read it. Premise: `ci.yml:41`,
  `merge-check.yml:54,102`; Decision 1. Check: CI green, and the `go install` log names v0.4.1.
- [x] 3. Take the v0.4.1 text of the three contracts. Premise: Decision 5. Check: `diff` against the v0.4.1 release
  files is empty.
- [x] 4. `scripts/ateam` wrapper, `task tools:ateam`, `.tools/` gitignored, Taskfile targets on the wrapper. Premise:
  `Taskfile.yml:30-42`; Decisions 2 and 3. Check: from a linked worktree, `scripts/ateam version` prints v0.4.1 with
  a different `ateam` first on `PATH`; with `.tools/bin` absent, the wrapper falls back to `PATH`.
- [x] 5. Point every hook and MCP config at the wrapper. Premise: the config lines in design Context; Decision 4.
  Check: `ateam doctor --check` passes, `hook_binary` resolves under `.tools/bin`, and hook latency stays under 10 ms.
- [x] 6. README Use section names `task tools:ateam`. `task verify` green locally and in CI. Archive into
  `openspec/specs/harness/` before the PR is marked ready.
