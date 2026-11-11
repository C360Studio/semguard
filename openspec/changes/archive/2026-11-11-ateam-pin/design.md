# Design: pinned ateam v0.4.1

## Context

- `.github/workflows/ci.yml:41` and `.github/workflows/merge-check.yml:54,102` install `ateam@v0.4.0`. The merge-check
  jobs call `$ATEAM` from `$RUNNER_TEMP` (`merge-check.yml:59,106`). `scripts/merge-check.sh:20` runs
  `${ATEAM:-ateam}`.
- `Taskfile.yml:30,33,36,39,42` call a bare `ateam`. `.claude/settings.json:9,19,29,39`, `.codex/hooks.json:8,18,28,38`,
  `.gemini/settings.json:16,26,36,46`, and `.agents/hooks.json:6,12,18` run `ateam hook …`. MCP: `.mcp.json:5`,
  `.codex/config.toml:5`, `.gemini/settings.json:54`.
- `which -a ateam` on the owner's laptop gives only `~/.local/bin/ateam`, which is v0.4.1 (66a0265). The repo has no
  `.agents/ateam.json`, no `.agents/ateam-version`, and no `.tools/` entry in `.gitignore`.
- `ateam doctor` v0.4.1 flags `.agents/contracts/{architect,developer,reviewer}.md`. Each is byte-identical to the v0.4.0
  release text. v0.4.1 adds one sentence: "If your protocol has no such numbered sections, read all of it" (ateam
  #371). The PR #17 reviewer ran `task verify` with v0.4.0, and it passed.
- ateam v0.4.1 `README.md` § Install documents the `.tools/bin` install, a wrapper that resolves the main checkout from
  the `.git` file, host hook variables, and the `ateam_version` key that doctor enforces. `docs/adoption.md:158`
  covers the key.
- A sibling repository in this workspace runs the same shape: a Taskfile pin, a main-checkout `.tools/bin`, and a
  `scripts/ateam` wrapper. That is the shape, and no code is copied.

## Goals / Non-Goals

Goals: one pin that CI and every local caller read, and that doctor enforces. Non-goals: a skills refresh and a
wholesale `ateam init --force`.

## Decisions

1. **Where the pin lives.** (a) `.agents/ateam.json` `ateam_version`, which the Taskfile reads with `jq` and CI reads
   with `jq`. (b) A Taskfile var with literal versions in the workflows. Option (b) leaves three or four copies to keep
   in step, and doctor cannot see it. Option (a) is ateam's own key: doctor fails on a running or hook binary that is
   not at the pin. **Choose (a).** Seam: a bump is one edit to `ateam.json` plus a fresh `task tools:ateam`. Skip the
   install, and verify fails loudly on the pin check.
2. **Install location.** (a) The main checkout's `.tools/bin`, resolved through `git rev-parse --git-common-dir` in the
   Taskfile. (b) A `.tools/bin` per worktree. Option (b) costs one install per worktree, and the agents create a
   worktree for every claim. **Choose (a).**
3. **What the wrapper does with no pinned install.** (a) It falls back to `ateam` on `PATH`. (b) It exits 127 with
   "run task tools:ateam". Option (b) breaks every hook in a fresh clone, so a session cannot start until the step is
   run. Option (a) keeps sessions alive. A wrong `PATH` binary is then caught by doctor's pin check in `task verify`,
   not at the hook. **Choose (a).** Forecloses: a hook can run the wrong release between installs, but verify will
   not pass.
4. **How hooks and MCP name the wrapper.** (a) Claude and Gemini use their project-dir variable. Codex, agy, and MCP
   use the relative `scripts/ateam` (README host table). (b) `sh -c 'exec "$(git rev-parse --show-toplevel)/scripts/ateam"'`
   works from any directory. (c) Keep the bare `ateam`. Option (b) forks `git`, about 11 ms measured upstream, which is
   past the 10 ms hook budget. Option (c) is the bug. **Choose (a).** Seam: start sessions at the checkout root
   (ateam #339). Under a host whose agent process stays in the main folder while the work moves to a worktree,
   hooks resolve the main checkout's wrapper, and that runs the same pinned binary.
5. **Contracts.** (a) Copy the three v0.4.1 contract texts by hand. (b) `ateam init --force`, which also rewrites
   skills and protocol documents that semguard has edited. **Choose (a).** The diff is three sentences.

## Risks / Trade-offs

- Doctor's pin check reads only the Claude hook command, so Codex and Gemini hooks could drift without a failure.
  Mitigation: all of them name the same wrapper, and review checks that.
- The skills under `.agents/skills/` stay at their older text. Doctor does not flag them today. A later release might.
- In CI the wrapper falls through to the `go install`ed `PATH` binary, which is the pinned release, so CI behavior is
  unchanged.

## Migration Plan

After merge, each clone runs `task tools:ateam` once. The global `~/.local/bin/ateam` can stay: nothing in the repo
calls it unless the pinned install is missing.

## Open Questions

None. Owner ruled on scope (2026-11-11, #19).
