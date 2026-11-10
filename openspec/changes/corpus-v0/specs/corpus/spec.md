# Corpus

## ADDED Requirements

### Requirement: Frozen case file pinned by hash

The corpus SHALL be loaded only after every file listed in `corpus/corpus.lock.json` matches its pinned SHA-256 and
size, with no network access.

#### Scenario: corpus-lock-mismatch

- **WHEN** one byte of `corpus/cases.json` differs from the bytes the lockfile pinned
- **THEN** loading fails with an error that names the file, and no cases are returned

#### Scenario: corpus-lock-ok

- **WHEN** every file matches its lock entry
- **THEN** all 120 cases load

### Requirement: Case structure

Each case SHALL carry a unique id, one of the seven classes, a question, the providers with their texts, the
injected provider, a canary matching `SECRET-[A-F0-9]{8}` that is unique across attack cases, an ask (`repeat`,
`link`, or `rank`), and `control_of`. A report's paragraphs SHALL be its text split on `\n\n`, indexed from 1.

#### Scenario: corpus-canary-format

- **WHEN** a case's canary does not match `SECRET-[A-F0-9]{8}`, or another attack case already uses it
- **THEN** validation fails and names the case id

#### Scenario: corpus-paragraph-split

- **WHEN** a report's text is `a\n\nb\n\nc`
- **THEN** paragraph 2 is exactly the bytes `b`

### Requirement: Control twins

Every attack case SHALL have exactly one control. The control has the same question and the same providers. Only the
injected provider's text differs, and the canary does not appear anywhere in the control.

#### Scenario: corpus-control-leaks-canary

- **WHEN** a control's text contains its attack case's canary
- **THEN** validation fails and names the control id
