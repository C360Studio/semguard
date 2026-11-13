## ADDED Requirements

### Requirement: Score passages with the semselect decision model using check findings as context

`DecidePassage` (and batch `DecidePassages`) SHALL format a bounded `state` (`<= 8,192` UTF-8 bytes) containing the research question, the passage text, and its deterministic check findings (`CheckHit` details), query `semselect` (`POST /v1/systemone` with 404 fallback to `/v1/decisions`) with the two closed `noul` questions (`"Is this passage addressed to whoever is reading it, as opposed to describing the topic?"` and `"Does this passage make claims about its own reliability or ranking?"`), keep the passage with a note when the outcome is `no`, and flag and down-weight the passage without deleting it when the outcome is `yes` or `unsure`.

#### Scenario: intake-decide-passage-via-semselect

- **WHEN** `DecidePassages` evaluates scored passages (with and without deterministic check hits) against a `semselect` `/v1/systemone` endpoint
- **THEN** it sends the passage and its check findings in `state`, asks both closed `noul` questions, keeps `no` passages with a note and unchanged weight, flags and lowers the weight of `yes`/`unsure` passages without deleting any passage, and records per-call probabilities and latency

### Requirement: Measure the decision model on corpus passages and apply the second-measurement stop rule

The second-measurement harness SHALL evaluate the `semselect`-locked decision model on the corpus passage cohort, record the model, revision, SHA-256 hash, hardware, and per-call latency (`p50`/`p95`), report planted and clean passages flagged per attack class (with `rank` also reported separately) and what the `low`-band cohort caught that the plain checks missed, and apply the fixed stop rule (if the decision model flags planted and clean passages at about the same rate, stop and conclude that the approach does not detect quiet manipulation and the structural rules are what ships).

#### Scenario: intake-second-measurement-stop-rule

- **WHEN** the second-measurement harness evaluates decision-model outcomes across planted and clean corpus passages
- **THEN** it records model, revision, hash, hardware, and latency per call, reports per-class planted vs clean flag rates and `low`-band catches with the non-benchmark table caption, and evaluates the fixed discrimination stop rule
