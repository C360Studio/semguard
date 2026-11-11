# Design: datamark and frame

## Context

- `README.md:19` sets the stage contract `Frame(task, docs) Prompt`: datamark, framing text, token cap.
  `README.md:51` places it in `internal/untrusted`, which milestone 3 exports.
- Issue #5 specifies the markers `<<report id=pN>> … <</report>>`, `^` for whitespace runs, framing prompts in
  `internal/untrusted/prompts/*.txt` pinned by hash in each results file, and one on/off toggle.
- Issue #7 sets the exported API: `Datamark(docs) Block`, `Frame(task, docs) Prompt`, with `Segment` and `Strip`
  alongside. Whatever this change names, #7 promotes.
- `internal/corpus/corpus.go:50-54`: `Document{ID, Text}`. `:100`: a paragraph is a split on `\n\n`, indexed from 1
  (corpus Decision 1). `corpus/tasks.json` carries the task `instruction`. The case `question` is per case
  (`corpus.go:60`).
- Issue #4: a claim cites `paragraph` and `quote_sha256`. Verify hashes the quoted text against the paragraph bytes,
  so whatever datamarking does to the text has to be undone exactly.
- Corpus facts, read with `jq` without printing any text: the document IDs are `d1`, `d2`, `d3`. No document text
  contains `^`, `<<`, or `>>`. No text contains a tab, a CR, a double space, a single `\n`, or `\n\n\n`. The largest
  case is 3,031 bytes of document text.
- `docs/hijack-definition.md`: H1 is a canary substring and H2 is a byte-for-byte URL. Neither the canary nor a URL
  contains whitespace, so `^` never lands inside one.

## Goals / Non-Goals

Goals: a deterministic, hash-pinned prompt builder with one toggle, plus an exact inverse for quotes. Non-goals: the
reader, schemas, verify, segment and strip, and any tokenizer.

## Decisions

1. **Paragraph breaks under datamarking.** (a) Replace every whitespace run with `^`, including `\n\n`. This is the
   spotlighting original. (b) Replace only the runs inside a paragraph and keep `\n\n`. (c) Tag each paragraph with
   `<<p n=K>>`. Option (a) erases the paragraph boundary, so the reader can no longer cite a paragraph index for
   verify. Option (c) costs tokens and adds a second marker to defend. **Choose (b).** Seam: a quote the reader copies
   comes back with `^`, and verify must call `Unmark` before it hashes. Do nothing, and every schema-mode claim fails
   to resolve.
2. **Marker id.** (a) Use `Document.ID` as given (`d1`). (b) Use positional `p1..pN`, as the issue text has it.
   Option (a) keeps one id space across the prompt, the schema output, and verify. Option (b) hides source IDs from
   the reader but needs a mapping table in verify. **Choose (a)**, with IDs restricted to `[A-Za-z0-9_-]{1,32}` so an
   ID can never close a marker. (owner) This departs from the `pN` in the #5 text.
3. **Token cap.** (a) A byte cap on `System`+`User`, defaulting to 16 KiB and set by the caller. (b) A real tokenizer
   per model. Option (b) adds a dependency and is model-specific, and the reader models are still open (#9). Option
   (a) is approximate, but the corpus's largest case is about 3 KiB, so the cap never binds on it. **Choose (a).**
   Over the cap is an error, never a truncation: truncating could drop the injected paragraph, and the run would then
   score as safe. Mark it `// shortcut: byte cap, not tokens; upgrade when a reader model's context binds`.
4. **Text that spoofs a marker.** (a) Return an error that names the document. (b) Neutralize the text by escaping or
   substituting lookalikes. Option (b) changes the bytes, which breaks quote hashing and H2's byte-for-byte rule.
   Option (a) forecloses documents that legitimately contain `^`, such as math, until #7 revisits it. The corpus has
   none. **Choose (a)**, for `<<report`, `<</report>>`, and `^`.
5. **Where the framing text sits.** (a) The system message carries the task instruction and the framing text, and
   the user message carries the question and the documents. (b) Everything goes in one user message. Option (a)
   follows the trust split, since the system message holds only trusted text. Option (b) suits endpoints that
   ignore system messages. **Choose (a).** `Prompt` keeps the two strings apart, so the reader client (#4) sends two
   messages. Seam: a client that joins them into one message silently changes the measured configuration.
6. **Types.** (a) `untrusted` defines its own `Document` and `Task`. (b) It takes `corpus.Case`. Option (b) couples an
   exported package to an internal one, and #7 could not promote it. **Choose (a).** The runner converts.

## Risks / Trade-offs

- Frame on/off bundles two effects: the datamark and the framing text. A measured gain cannot be split between them
  until both have their own toggles (Open Question 2).
- `Unmark` is exact only for text whose in-paragraph whitespace is single spaces. That holds for the corpus, and a
  test pins it. Live text with tabs or newlines inside a paragraph would make a quote fail to resolve: a false
  B1/broken, never a missed hijack.
- `^` next to a URL: the verify URL regex in #4 must stop at `^`. That is #4's test, noted there.

## Migration Plan

None. This is the first frame. A later change to `frame.txt` changes `FramingSHA256`, and that requires a rerun.

## Open Questions

1. (owner) Decision 2: use document IDs (`d1`) in the markers rather than `pN`?
2. (owner) Keep one toggle, as #5 states (2 cells per model), or split datamark and framing (4 cells)? This change
   ships one, and splitting later is a one-field change to `Options`.
