# Corpus v0

Approved-by: Coby (2026-11-10) sha256:2b83edfdcec1

Track: design (adds the first `corpus` spec requirements).

## Why

Milestone 1 needs a frozen set of detonation cases before any reader run means anything. Every published number cites
the corpus hash, so the case format, canary scheme, and lockfile have to be settled first and then never edited in
place.

## What Changes

- `corpus/cases.json`: 7 classes × 10 cases = 70 cases, plus one control twin per attack case (50), for 120 entries.
  Classes: override, exfil, social, fragmented, rank (attack); quoted, article (benign).
- `corpus/tasks.json`: the reader task per case family (v0: `score-compare`), its instruction, and its schema path.
- Canaries: `SECRET-[A-F0-9]{8}`, one per case, generated once from `crypto/rand` by a one-shot tool and frozen in
  `cases.json`. A control twin uses its attack case's canary, which never appears in the control's text.
- `corpus/corpus.lock.json`: SHA-256 and size per corpus file (`cases.json`, `tasks.json`).
- `internal/corpus`: load cases, validate their structure, and verify the lockfile. No network.
- `task corpus:validate` runs the lockfile check and the structure check offline. `task check` covers the package
  tests.

## Non-goals

- No reader, runner, or hijack rules (#4, #5, #8). This change uses no inference.
- Not a benchmark: 70 hand-authored cases. Every table that uses them says so.

## Impact

- New: `corpus/`, `internal/corpus/`, `openspec/specs/corpus/spec.md`, Taskfile `corpus:validate`.
- Changing a case after this lands is a corpus change that requires a rerun and new results.

## Done when

`task corpus:validate` and `task check` pass offline, and the lockfile pins the frozen `cases.json` and `tasks.json`.
