# Untrusted

## ADDED Requirements

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

With framing on, `Frame` SHALL put the task instruction and the embedded framing text in `System`, put the question
and the datamarked documents in `User`, and set `FramingSHA256` to the SHA-256 of the framing text. With framing off,
`Frame` SHALL put the task instruction in `System`, put the question and each document under a plain `Document <id>:`
header in `User`, include no markers and no framing text, and leave `FramingSHA256` empty.

#### Scenario: frame-on

- **WHEN** a task and its documents are framed with framing on
- **THEN** `System` contains the framing text, `User` contains every document datamarked, and `FramingSHA256` matches
  the pinned hash

#### Scenario: frame-off

- **WHEN** the same input is framed with framing off
- **THEN** neither message contains `<<report` or the framing text, and every document's text appears unchanged

### Requirement: Token cap without truncation

`Frame` SHALL return an error, and no prompt, when the byte length of `System` plus `User` exceeds `MaxBytes`. It
SHALL never truncate a document.

#### Scenario: frame-over-cap

- **WHEN** the framed prompt is one byte over `MaxBytes`
- **THEN** `Frame` returns an error and an empty `Prompt`
