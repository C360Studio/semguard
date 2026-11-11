# Datamark and frame

Approved-by: Coby (2026-11-11) sha256:f2b56d57a20c

Track: design (adds the first `untrusted` spec requirements and the surface milestone 3 exports, #7).

## Why

The frame stage is the cheapest defense in the pipeline. It is code: no model, no service. Milestone 1 has to measure
it on and off against the corpus before a gate or an audit stage can be said to earn its place. The reader client
(#4) and the runner (#8) both consume its `Prompt`, so its shape has to settle first.

## What Changes

- `internal/untrusted`: its own `Document{ID, Text}` and `Task{Instruction, Question}` (it does not import
  `internal/corpus`, because #7 exports this package).
- `Datamark(docs) (string, error)`: wraps each document in `<<report id=ID>>` … `<</report>>` and replaces each
  whitespace run inside a paragraph with `^`. The `\n\n` paragraph breaks stay, so a reader can still cite a paragraph
  by its index.
- `Unmark(s) string`: the inverse for quoted text (`^` becomes a space), which verify (#4) needs before it hashes a
  quote.
- `Frame(task, docs, opts) (Prompt, error)`: `Prompt{System, User, FramingSHA256}`. Two independent switches in
  `Options` (owner ruling, 2026-11-11), so the matrix has four cells per model:
  - `Datamark`: on, the user message carries the datamarked documents; off, each document sits under a plain
    `Document <id>:` header.
  - `Framing`: on, the system message adds the fixed framing text from `internal/untrusted/prompts/frame.txt`
    (embedded, pinned by SHA-256) after the task instruction; off, no framing text and `FramingSHA256` is empty.
    The text names "the documents in the user message", never the markers, so one file serves both datamark states.
- Token cap: a byte cap on `System`+`User`. Over the cap, `Frame` returns an error and no prompt. It never truncates.
- A document whose text already contains `<<report`, `<</report>>`, or `^`, or whose ID is outside
  `[A-Za-z0-9_-]{1,32}`, is an error that names the document.

## Non-goals

- No reader, schema, or verify code (#4). No segment or strip (#7). No runner (#8). No inference.
- No tokenizer: the cap counts bytes (design Decision 3).

## Impact

- New: `internal/untrusted/`, `openspec/specs/untrusted/spec.md`.
- Every results file records `FramingSHA256`, so editing `frame.txt` later is a prompt change that requires a rerun.

## Done when

`task verify` passes offline. Each spec scenario has a test that cites it. The Datamark/Unmark round trip is
fuzzed and has been shown able to fail with a non-destructive mutation check. The framing text's hash is pinned in a
test.
