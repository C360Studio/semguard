# untrusted Specification

## Purpose
The frame stage: datamark untrusted documents and assemble the reader prompt, with the datamark and the framing text
as independent switches and a size cap that never truncates. Design: `openspec/changes/archive/2026-11-11-untrusted-frame/design.md`.
## Requirements
### Requirement: Datamark documents

`Datamark` SHALL wrap each document in `<<report id=ID>>` and `<</report>>` in input order, replace every whitespace
run inside a paragraph with `^`, keep each `\n\n` paragraph break, and prefix each paragraph with `[n] `, its 1-based
index as `verify` counts it. `Unmark` SHALL replace each `^` with one space.

#### Scenario: datamark-paragraphs-kept

- **WHEN** a document with N paragraphs is datamarked
- **THEN** its marked body splits on `\n\n` into N paragraphs, paragraph i starts with `[i] `, and none of them
  contains a whitespace character after that label

#### Scenario: datamark-round-trip

- **WHEN** a paragraph whose whitespace is single spaces is datamarked and then unmarked
- **THEN** the result after its `[n] ` label equals the original paragraph byte for byte

### Requirement: Refuse marker spoofing

`Datamark` and `Frame` SHALL return an error that names the document, and no output, when a document's text contains
`<<report`, `<</report>>`, or `^`, or when its ID does not match `[A-Za-z0-9_-]{1,32}`.

#### Scenario: datamark-spoofed-close

- **WHEN** a document's text contains `<</report>>`
- **THEN** an error names that document and no marked text is returned

#### Scenario: datamark-bad-id

- **WHEN** a document's ID contains `>`
- **THEN** an error names that document

### Requirement: Frame the prompt

`Frame` SHALL put the task instruction in `System` and the question in `User`, under two independent switches.
With `Datamark` on, `User` SHALL carry the documents datamarked. With it off, `User` SHALL carry each document
unchanged under a plain `Document <id>:` header and no markers, except that each paragraph is prefixed with `[n] `,
its 1-based index as `verify` counts it. With `Framing` on, `System` SHALL add the embedded
framing text and `FramingSHA256` SHALL be its SHA-256. With it off, neither message SHALL contain the framing text, and
`FramingSHA256` SHALL be empty.

#### Scenario: frame-both-on

- **WHEN** a task and its documents are framed with both switches on
- **THEN** `System` contains the framing text, `User` contains every document datamarked, and `FramingSHA256` matches
  the pinned hash

#### Scenario: frame-datamark-only

- **WHEN** the same input is framed with `Datamark` on and `Framing` off
- **THEN** `User` contains every document datamarked, `System` lacks the framing text, and `FramingSHA256` is empty

#### Scenario: frame-framing-only

- **WHEN** the same input is framed with `Datamark` off and `Framing` on
- **THEN** `User` contains no `<<report` and every document's text unchanged apart from its `[n] ` paragraph labels,
  and `System` contains the framing text

#### Scenario: frame-both-off

- **WHEN** the same input is framed with both switches off
- **THEN** neither message contains `<<report` or the framing text, and every document's text appears unchanged
  apart from its `[n] ` paragraph labels

#### Scenario: frame-paragraph-labels

- **WHEN** documents are framed under any combination of the two switches
- **THEN** every paragraph carries a `[n] ` label, and `verify.Verify` accepts a quote from the text after label n as a
  quote from paragraph n

### Requirement: Token cap without truncation

`Frame` SHALL return an error, and no prompt, when the byte length of `System` plus `User` exceeds `MaxBytes`. It
SHALL never truncate a document.

#### Scenario: frame-over-cap

- **WHEN** the framed prompt is one byte over `MaxBytes`
- **THEN** `Frame` returns an error and an empty `Prompt`

### Requirement: Segment paragraphs with byte offsets

`Segment` SHALL split `text` on `\n\n` into a slice of `Paragraph{Index, Text, Start, End}` where `Index` is 1-based, `Text` is identical to `corpus.Paragraphs(text)[Index-1]`, and `text[Start:End] == Text`.

#### Scenario: segment-paragraphs-and-offsets

- **WHEN** `Segment` is called on a multi-paragraph string
- **THEN** it returns one `Paragraph` per `\n\n`-separated segment with 1-based `Index` and exact `Start`/`End` byte offsets into the input string

### Requirement: Strip unsafe characters and map offsets

`Strip` SHALL normalize `\r\n` and `\r` to `\n`, remove Unicode format/zero-width characters and control characters (other than `\n` and `\t`), and remove reserved datamark sequences (`^`, `<<report`, `<</report`), returning the cleaned string and an `OffsetMap` whose `OriginalOffset` and `Span` map byte offsets in the cleaned string back to their source byte offsets in the original input.

#### Scenario: strip-allows-datamark-and-maps-offsets

- **WHEN** `Strip` is called on text containing zero-width characters, CRLF line endings, `^`, or `<<report` / `<</report>>` markers
- **THEN** the returned string passes `Datamark` and `Frame` without error, and `OffsetMap.OriginalOffset(i)` points to the exact source byte in the unstripped input for every remaining byte `i`


