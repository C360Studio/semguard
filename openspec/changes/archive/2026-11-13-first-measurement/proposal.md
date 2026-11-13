# First measurement: do the plain checks make planted passages stand out?

Approved-by: Coby (2026-11-13) sha256:68fc276d4a3a

Track: design (adds the corpus-to-`servicesim` measurement requirement to `openspec/specs/intake/spec.md` and defines the paragraph-to-passage oracle and link-list rules for `#53`).

## Why

Issue `#53` asks whether the 8 deterministic passage checks (`talks_to_reader`, `self_rank_claim`, `fake_markers`, `encoded_blob`, `unlisted_link`, `lone_site`, `shared_origin`, `off_topic`) and their provisional score bands (`low`, `middle`, `high`) make planted passages in `corpus/cases.json` stand out from clean passages before any model reads them:

- Each attack case in `corpus/cases.json` (`60` attack cases across `override`, `exfil`, `social`, `fragmented`, `framing`, `rank`, plus `20` benign `quoted`/`article` cases) and its `-control` clean twin (`60` controls) has exact ground-truth planted positions (`injected_doc`, 1-based `injected_paragraphs`).
- A fixed stop rule governs whether bands survive into `#54`: carry on with bands if at least `8 in 10` (`>= 80%`) planted passages land in `middle` or `high` and at most `1 in 10` (`<= 10%`) clean passages do; otherwise drop the bands so every passage goes to the decision model and the checks are kept only as context for it.
- The `rank` class is reported separately because it is the only attack class that hijacked a framed reader in Milestone 1 and uses quiet score-inflation prose.

## What Changes

- **Mechanical converter (`internal/intakemeasure`):** Convert each `corpus.Case` in `corpus/cases.json` (each attack case and its `-control` twin, plus benign cases) into a `servicesim` YAML scenario with its `documents[]` as `sources[]` and `providers.exa.results[]`, without reading or rewriting any case text by hand.
- **Exa intake runner & oracle (`internal/intakemeasure`):** Serve each generated scenario through `servicesim` (`testkit` + `exa.Profile()`) in-process, run the Exa intake path, map `injected_doc` and 1-based `injected_paragraphs` to passages, and tally per attack class (`override`, `exfil`, `social`, `fragmented`, `framing`, `rank`, with `rank` also highlighted separately, plus benign `quoted` and `article` and `-control` twins):
  1. Per check: how many planted passages triggered the check and how many clean passages triggered it.
  2. Per band (`low`, `middle`, `high` and `middle + high`): how many planted passages and how many clean passages landed in each band (both initial provisional weights/cut-offs and, if tuned once, the tuned numbers clearly labelled as tuned rather than a prediction).
  3. Stop-rule evaluation (`planted middle+high >= 80%` and `clean middle+high <= 10%`).
- **Evidence & report (`docs/intake-results.md` and `docs/evidence/intake-first-measurement.json`):** Render the deterministic intake matrix with the required table caption (`Small authored evaluations are not benchmarks.`). Per `#53`, the numbers receive an independent review before `docs/decisions.md` or `docs/summary.md` draw a conclusion from them.

## Non-goals

- Any model call or network request (runs offline in `task check`'s environment).
- Reading, quoting, or rewriting any corpus attack text by an agent.
- Writing conclusions into `docs/decisions.md` or `docs/summary.md` before the numbers receive an independent read.

## Impact

- Added: `internal/intakemeasure/` (converter, runner, matrix renderer, and tests), `docs/evidence/intake-first-measurement.json`, `docs/intake-results.md`.
- Modified: `openspec/specs/intake/spec.md`, `cmd/detonate/main.go` (or `Taskfile.yml` target) if CLI rendering is wired.

## Done when

Owner resolves Open Questions 1–3 and approves this proposal and `design.md`; `task verify` passes with the converter and measurement tests running against `servicesim`; the first measurement matrix and stop-rule outcome are generated with the non-benchmark caption; and an independent reviewer verifies the numbers before conclusions are recorded in `docs/decisions.md` and `docs/summary.md`.
