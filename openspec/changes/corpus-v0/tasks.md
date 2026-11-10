# Tasks

- [ ] 1. Owner approves proposal.md and design.md, (Open Question 1 was ruled on 2026-11-10).
  Hold: OPEN-Q proposal awaits owner approval
- [ ] 2. `internal/corpus`: case types, `Load(dir)` with lock verification, `Paragraphs(text)`, and the structural
  validator. Premise: design Decisions 1, 2, and 5. Check: one test per scenario, citing it with `//aip:verifies`.
- [ ] 3. Canary tool: a one-shot run writes the canaries, then they are frozen. Premise: Decision 3. Check: the
  validator rejects a duplicate or malformed canary.
- [ ] 4. Author the 70 cases and 50 controls LLM-drafted, then hand-edited, per Decision 4; record the drafting model and prompts. Check: the validator passes.
- [ ] 5. Write `corpus/corpus.lock.json` and add `task corpus:validate`. Check: flipping one byte in `cases.json`
  fails it, confirmed with a non-destructive `cp` plus SHA-256 check.
- [ ] 6. Archive this change into `openspec/specs/corpus/` before the PR is marked ready.
