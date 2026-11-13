## ADDED Requirements

### Requirement: Task-independent quote and claim verification

`QuoteInPassage` SHALL verify that `quote` (after normalizing datamark `^` to space) appears as a substring of `passageText` and, when `quoteSHA256` is non-empty, that its lowercase hex SHA-256 matches the normalized quote's SHA-256. `VerifyClaim` SHALL verify that `docID` resolves in `docs`, `paragraph` is a valid 1-based index into `strings.Split(doc.Text, "\n\n")`, and `QuoteInPassage` succeeds on that paragraph. `Verify` SHALL delegate claim resolution to `VerifyClaim`, and `verify.Document` SHALL be unified with `untrusted.Document`.

#### Scenario: verify-quote-in-passage

- **WHEN** `QuoteInPassage` or `VerifyClaim` is called on a passage or paragraph with a matching quote (with or without `^` datamarks) and optional SHA-256
- **THEN** it returns `nil`, and when the quote or hash does not match it returns a descriptive error
