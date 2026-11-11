# Segment, strip, and export the module API

Approved-by:

Track: design (adds `Segment` and `Strip` requirements to `openspec/specs/untrusted/spec.md` and promotes `untrusted` and `verify` to exported module packages).

## Why

Issue #7 promotes `internal/untrusted` and `internal/verify` to the public module surface (`github.com/c360studio/semguard/untrusted` and `github.com/c360studio/semguard/verify`) and adds `Segment` and `Strip` to `untrusted` so external consumers can sanitize raw source text, segment paragraphs with byte offsets (`Paragraph`), datamark (`Datamark` / `Block`), frame (`Frame`), and verify (`Verify`, `AllowedLinks`) without writing their own text-hygiene or verification glue.

## What Changes

- `untrusted` (moved from `internal/untrusted`):
  - `Segment(text string) []Paragraph` where `Paragraph{Index int, Text string, Start int, End int}` splits on `\n\n` with 1-based `Index` and byte offsets `[Start, End)` into `text`, matching `corpus.Paragraphs` and `verify` claim indexing.
  - `Strip(text string) (string, OffsetMap)`: normalizes or removes characters and sequences that would fail `Datamark`/`Frame` or hide injections (such as CRLF `\r\n` → `\n`, zero-width/invisible format characters and control characters, or marker characters `^`, `<<report`, `<</report>>`) while returning an `OffsetMap` (`OriginalOffset(cleanByteOffset int) int`) mapping byte offsets in the cleaned string back to the original `text`.
  - `Block` type (`type Block = string` or `type Block string`) returned by `Datamark(docs []Document) (Block, error)` (matching #7 `Datamark(docs) Block` while preserving error handling).
  - `Frame(t Task, docs []Document, o Options) (Prompt, error)` and `Unmark(s string) string`.
- `verify` (moved from `internal/verify`):
  - Exported at `github.com/c360studio/semguard/verify` with `Verify(raw string, docs []Document) (Record, []Violation)`, `AllowedLinks(output string, docs []Document) []Violation`, and `ParseFree(raw string, docs []Document) Record`.
- Update callers (`cmd/detonate`, `internal/report`), `Taskfile.yml` (`lint` `gofmt` paths), and `README.md` layout table.

## Non-goals

- No gate client (`cmd/guard` / milestone 2, #10, which is blocked on #6 and M1 results #9).
- No consumer-specific integration docs or adapters (`AGENTS.md:47`).

## Impact

- Moved: `internal/untrusted` → `untrusted`, `internal/verify` → `verify`.
- Updated: `cmd/detonate/main.go`, `internal/report/report.go`, `Taskfile.yml`, `README.md`, `openspec/specs/untrusted/spec.md`, `openspec/specs/verify/spec.md`.

## Done when

`task verify` passes offline. Every `segment-*` and `strip-*` scenario in `openspec/specs/untrusted/spec.md` is cited by a unit test, `Strip` + `OffsetMap` and `Segment` are covered by `testing.F` fuzzing against an independent oracle, and non-trivial branches pass `task mutate:check`.
