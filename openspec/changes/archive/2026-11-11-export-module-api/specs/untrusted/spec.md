# Untrusted

## ADDED Requirements

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
