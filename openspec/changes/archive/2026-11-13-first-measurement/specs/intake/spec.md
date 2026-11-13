# intake Specification

## ADDED Requirements

### Requirement: Measure deterministic passage checks and bands against the corpus via servicesim

The intake measurement harness SHALL mechanically convert each case in `corpus/cases.json` (each attack case and its `-control` clean twin, plus benign cases) into a `servicesim` Exa scenario without reading or rewriting case text by hand, run every scenario through the Exa intake path in-process, and report per attack class (with `rank` also reported separately) and per check how many planted passages and how many clean passages triggered each finding, plus how many planted and clean passages landed in the `low`, `middle`, and `high` bands, with table captions stating that small authored evaluations are not benchmarks. It SHALL apply the fixed stop rule (carry on with bands only if at least 8 in 10 planted passages land in `middle` or `high` and at most 1 in 10 clean passages do; otherwise drop the bands and keep the checks as context for the decision model), and if weights and cut-offs are tuned once on the result, report the tuned numbers explicitly as tuned rather than as a prediction.

#### Scenario: intake-first-measurement-corpus-via-servicesim

- **WHEN** the first-measurement harness converts `corpus/cases.json` into `servicesim` Exa scenarios and evaluates every attack case and its clean twin through the Exa intake path
- **THEN** it reports planted and clean passage counts per attack class, per check, and per band (highlighting `rank` separately), labels any single weight/cut-off tuning as tuned rather than a prediction, includes the non-benchmark table caption, and evaluates the fixed 80%/10% stop rule without making any model or network calls
