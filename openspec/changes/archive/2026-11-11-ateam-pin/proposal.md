# Pinned ateam v0.4.1

Approved-by: Coby (2026-11-11) sha256:d8b001cd824d

Track: design (touches gate files: CI workflows, hook configs, agent contracts; adds the first `harness` requirement).

## Why

Local `task verify` fails at `ateam doctor` while CI passes. CI installs ateam v0.4.0, and every local caller (Taskfile,
four hook configs, three MCP registrations) runs a bare `ateam`. On the owner's machine, that resolves to a global v0.4.1,
whose doctor wants the v0.4.1 contract text. The repo has no single pin, so the local gate and the CI gate silently
run different releases (#19). Owner ruling, 2026-11-11: bump to v0.4.1 and adopt a per-repo pinned install.

## What Changes

- `.agents/ateam.json`: `{"version":1,"ateam_version":"v0.4.1"}`, the one pin. ateam v0.4.1's doctor fails when the
  running binary or the Claude hook binary is not exactly this release.
- `.github/workflows/ci.yml` and `merge-check.yml`: the three `go install …@v0.4.0` lines read the pin from that file.
- `.agents/contracts/{architect,developer,reviewer}.md`: take the v0.4.1 text (one sentence each).
- `Taskfile.yml`: `tools:ateam` installs the pin into the main checkout's gitignored `.tools/bin`, which every linked
  worktree shares, and skips when it is already at the pin. All `ateam` targets call `scripts/ateam`.
- `scripts/ateam`: a committed POSIX `sh` wrapper. It runs the main checkout's `.tools/bin/ateam`, else the worktree's,
  else `ateam` on `PATH`. It finds the main checkout from the `.git` file without forking `git`, which keeps hooks in
  their 10 ms budget.
- Hooks and MCP: `.claude/settings.json` uses `"$CLAUDE_PROJECT_DIR"/scripts/ateam`, `.gemini/settings.json` uses
  `"$GEMINI_PROJECT_DIR"/scripts/ateam`, and `.codex/hooks.json`, `.agents/hooks.json`, `.mcp.json`,
  `.codex/config.toml`, and the Gemini MCP entry use `scripts/ateam`.
- `.gitignore`: `.tools/`. `README.md` Use section: run `task tools:ateam` once per clone.

## Non-goals

- No refresh of `.agents/skills/` to v0.4.1. Doctor does not flag them, and three of them are local edits.
- No `ateam init --force`. It would overwrite the harness documents wholesale.
- No change to `scripts/merge-check.sh` logic. It already honors `$ATEAM`.

## Impact

- One-time step per clone: `task tools:ateam`. Without it, the wrapper falls back to `PATH`, and doctor's pin check
  fails `task verify` if that binary is the wrong release (design Decision 3).
- Sessions must start at the checkout root (ateam #339), since a relative `scripts/ateam` resolves against the
  hook's working directory.

## Done when

`task verify` passes locally with the global ateam at any version, using the pinned v0.4.1 from `.tools/bin`, and CI
passes. A hook or MCP call made from a linked worktree runs the main checkout's pinned binary. Doctor reports `pin`
matching v0.4.1.
