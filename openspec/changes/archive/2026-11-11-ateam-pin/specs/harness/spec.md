# Harness

## ADDED Requirements

### Requirement: One pinned ateam release

The repository SHALL declare one ateam release in `.agents/ateam.json` `ateam_version`. CI, the Taskfile, every hook
config, and every MCP registration SHALL run that release, and `task verify` SHALL fail when the binary that runs is not
exactly that release.

#### Scenario: harness-pin-mismatch

- **WHEN** the ateam binary that `task verify` runs is not the release in `ateam_version`
- **THEN** `ateam doctor --check` fails and `task verify` stops

#### Scenario: harness-worktree-shares-pin

- **WHEN** a linked worktree calls `scripts/ateam` and the main checkout has the pinned release in `.tools/bin`
- **THEN** that pinned binary runs, whatever `ateam` comes first on `PATH`

#### Scenario: harness-fallback-to-path

- **WHEN** no `.tools/bin/ateam` exists in the main checkout or the worktree
- **THEN** `scripts/ateam` runs `ateam` from `PATH`
