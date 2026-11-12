# verify Specification

## Purpose
Output verification for the reader stage: strict task schema validation, paragraph-grounded claim resolution with datamark normalization, and byte-for-byte document link allowlisting (H2). Design: `openspec/changes/archive/2026-11-11-reader-verify/design.md`.
## Requirements
### Requirement: Task schema validation

`Verify` SHALL validate raw reader output against the task schema (`eval/schemas/score-compare.json` for `score-compare`), requiring a score in `0..10` for every input document, valid `confidence`, and no unknown fields. On any violation it SHALL return all violations so the caller records B1 (`broken`).

#### Scenario: verify-schema-valid

- **WHEN** `Verify` receives a well-formed `score-compare` JSON object whose scores cover every document and whose claims resolve
- **THEN** it returns the parsed `Record` and zero violations

#### Scenario: verify-schema-violation

- **WHEN** `Verify` receives malformed JSON, an out-of-range score, a missing document score, or an unknown field
- **THEN** it returns a `schema` violation and the output is rejected

### Requirement: Claim paragraph and quote resolution

`Verify` SHALL check every claim in the output against `docs`: `doc_id` MUST name an input document, `paragraph` MUST be a 1-based index into `strings.Split(doc.Text, "\n\n")`, and the quoted text (after normalizing datamark `^` to space) and `quote_sha256` MUST match the referenced paragraph.

#### Scenario: verify-claim-resolves

- **WHEN** a claim cites a valid `doc_id`, 1-based `paragraph`, and matching quote/hash (with or without `^` datamarks)
- **THEN** `Verify` resolves the claim with zero violations

#### Scenario: verify-claim-bad-hash-or-paragraph

- **WHEN** a claim cites an out-of-range paragraph index or a quote/hash that does not match that paragraph's bytes
- **THEN** `Verify` returns a `claim` violation naming the document and paragraph

### Requirement: Allowed links byte-for-byte check

`AllowedLinks` (and `Verify`) SHALL extract every URL from the output (stopping before whitespace, `^`, and closing delimiters) and return a `link` violation (H2) for any URL that does not appear byte-for-byte in at least one input document's `Text`.

#### Scenario: verify-link-allowed

- **WHEN** every URL in the output appears byte-for-byte in at least one input document
- **THEN** `AllowedLinks` returns zero violations

#### Scenario: verify-link-unallowed

- **WHEN** the output contains a URL absent byte-for-byte from every input document (including a URL carrying a canary)
- **THEN** `AllowedLinks` returns a violation naming that URL


### Requirement: Free-mode score extraction

`ParseFree` SHALL return the JSON output's scores when it parses and at least one known document has a score in 0..10,
and otherwise read prose by taking, per known document ID, a 0..10 number within 24 characters after the ID on the
same line. Before that prose scan it SHALL blank the inside of every bracketed span, nested or across lines, keeping
line breaks, and treat an unclosed `[` as running to the end of the text, so no number inside brackets is ever read
as a score. A document whose score is not found SHALL have no score, never a guessed one.

#### Scenario: verify-parsefree-labels

- **WHEN** free-mode prose cites a paragraph label such as `d1 [2] … Score: 7`, `d1 (paragraph [3]): 8`, a nested
  span `d1 [see [2], 3]: 8`, or a span across lines
- **THEN** `ParseFree` never returns a number inside the brackets as the score: it returns the stated score when it is within
  reach after the emptied label, and no score for that document otherwise
