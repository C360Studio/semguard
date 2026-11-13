## ADDED Requirements

### Requirement: Documented Strip-then-Frame path

`untrusted` SHALL document and test via an executable `Example` how a caller sanitizes raw untrusted text with `Strip` before calling `Frame`, including what `Strip` removes (`\r`, control/format characters, `^`, `<<report`, `<</report`).

#### Scenario: untrusted-strip-then-frame-example

- **WHEN** raw document text containing `^` or `<<report` is passed through `Strip` and then `Frame`
- **THEN** `Frame` succeeds without a marker-spoofing error
