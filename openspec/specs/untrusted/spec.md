# untrusted Specification

## Purpose
The frame stage: datamark untrusted documents and assemble the reader prompt, with the datamark and the framing text
as independent switches and a size cap that never truncates. Design: `openspec/changes/archive/2026-11-11-untrusted-frame/design.md`.
## Requirements
### Requirement: Datamark documents

`Datamark` SHALL wrap each document in `<<report id=ID>>` and `<</report>>` in input order, replace every whitespace
run inside a paragraph with `^`, and keep each `\n\n` paragraph break. `Unmark` SHALL replace each `^` with one space.

#### Scenario: datamark-paragraphs-kept

- **WHEN** a document with N paragraphs is datamarked
- **THEN** its marked body splits on `\n\n` into N paragraphs, and none of them contains a whitespace character

#### Scenario: datamark-round-trip

- **WHEN** a paragraph whose whitespace is single spaces is datamarked and then unmarked
- **THEN** the result equals the original paragraph byte for byte

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
unchanged under a plain `Document <id>:` header and no markers. With `Framing` on, `System` SHALL add the embedded
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
- **THEN** `User` contains no `<<report` and every document's text unchanged, and `System` contains the framing text

#### Scenario: frame-both-off

- **WHEN** the same input is framed with both switches off
- **THEN** neither message contains `<<report` or the framing text, and every document's text appears unchanged

### Requirement: Token cap without truncation

`Frame` SHALL return an error, and no prompt, when the byte length of `System` plus `User` exceeds `MaxBytes`. It
SHALL never truncate a document.

#### Scenario: frame-over-cap

- **WHEN** the framed prompt is one byte over `MaxBytes`
- **THEN** `Frame` returns an error and an empty `Prompt`

