# Design: Second Measurement — Fast Decision Model on Passages (#54)

## Context

- `internal/audit/audit.go:116-334`: `SemselectAuditor` posts `semselectRequest{Model, State, Questions}` to `POST /v1/systemone` with a 404 fallback to `/v1/decisions` (`audit.go:299-308`), enforcing `DefaultMaxStateBytes = 8192` (`audit.go:30`) and `DefaultMaxBodyBytes = 32 << 10` (`audit.go:32`). In Milestone 1 (`docs/decisions.md:60-80`), `semselect` at revision `152feca58e447b332119f2751e629e7749628de5` (`models.lock.json`: `semselect-kev-4b`, `ggml-org/Kev-4B-GGUF@d924f2e2c387`, `Kev-4B-Q4_K_M.gguf`, `sha256:33ae6b18926502b2209a1bf7d3b61a350d65938441515c686bb87d226a19eff9`) scored document-blind reader records in `2.6`–`3.9` s/call with zero discrimination.
- `intake/score.go:48-80,85-227`: `CheckAndScore` attaches `Hits []CheckHit` (`intake/checks.go:44-49`: `Check`, `Weight`, `Detail`), `Checks []CheckName`, `Score`, `Band`, `Flagged`, and `WeightMult` to every `ScoredPassage` without deleting any passage.
- `internal/intakemeasure/measure.go:411-522,656-685` & `docs/intake-results.md:8-14`: In `#53`, paragraph-level evaluation over `corpus/cases.json` (`80` planted paragraphs across `60` attack cases; `539` clean paragraphs across `60` `-control` twins) placed `35/80` (`43.8%`) planted and `24/539` (`4.5%`) clean twin paragraphs in `middle+high`, leaving `45/80` planted (`social` `10`, `fragmented` `30`, `rank` `5`) and `515/539` clean twin paragraphs in `low`. The `#53` stop rule returned `drop_bands_keep_checks_as_context`.

## Goals / Non-Goals

- **Goals:**
  1. Add a passage-level decision-model stage in `intake` that sends each passage and its check findings (`CheckHit` details) to `semselect` in one request with the two closed `noul` questions specified in `#54`.
  2. Keep every passage: `no` records a note and leaves `WeightMult` unchanged; `yes` or `unsure` flags the passage, lowers `WeightMult`, and reports the scores and latency.
  3. Measure discrimination on the corpus passage cohort with full provenance (`model`, `revision`, `sha256`, `hardware`, `p50`/`p95` latency per call), evaluate the `#54` stop rule, and publish `docs/evidence/intake-second-measurement.json` and `docs/intake-results.md` before an independent read.
- **Non-Goals:**
  - Deleting passages, adopting an unpinned model outside `semselect`'s lockfiles, or editing `docs/decisions.md` / `docs/summary.md` before the independent review.

## Decisions

### 1. Which `semselect`-locked decision model to measure (Owner Decision Needed in `#54`)
- **Option A (Recommended):** Measure `semselect`'s primary locked model `semselect-kev-4b` (`models.lock.json`: `ggml-org/Kev-4B-GGUF@d924f2e2c3872da8b8aaf3eb4453b4126deceb79`, `Kev-4B-Q4_K_M.gguf`, `sha256:33ae6b18926502b2209a1bf7d3b61a350d65938441515c686bb87d226a19eff9`) on local Apple Silicon Metal (`scripts/metal.py serve` at `127.0.0.1:8084` over `llama-server` `6c59c40076c0`), where both `noul` questions share one state pass (`~1.0`–`2.5` s/passage). This uses the exact `semselect` service lockfile and tests whether reading the passage + check findings gives the 4B decision model the discrimination it lacked when document-blind in Milestone 1.
  - *Cost / blast radius:* Zero new model downloads (`Kev-4B-Q4_K_M.gguf` and `.native` Metal runtime are already cached and verified in `/Users/coby/Code/c360/semselect`).
  - *Forecloses:* Does not claim millisecond-class latency; latency is recorded honestly alongside discrimination.
- **Option B:** Measure both `semselect-kev-4b` (Metal `models.lock.json`) and the isolated millisecond-class candidate `semselect-julia-1` (`eval/julia/models.lock.json`: `ggml-org/Julia-1-GGUF@16fee1794920`, `Julia-1-Q8_0.gguf`, `sha256:1ea6a7e87156...`, `~159` ms CPU median) against `/v1/systemone`.
  - *Cost / blast radius:* Runs two model cells; note that `semselect`'s Julia diagnostic (`eval/julia/README.md`) scopes Julia as an isolated 1,024-token CPU candidate that does not replace `models.lock.json`.
- **Option C:** Measure `semselect-julia-1` (`Julia-1-Q8_0.gguf`) alone.
  - *Cost / blast radius:* Fast (`~160` ms/call), but if it fails to discriminate, we cannot tell whether the passage-reading approach failed or Julia's small 168 MB head failed.

### 2. Passage cohort after `#53`'s `drop_bands_keep_checks_as_context` outcome
- **Option A (Recommended):** Evaluate all `80` planted paragraphs (`35` `middle+high` + `45` `low`, covering all `10` `social`, `30` `fragmented`, and `10` `rank` passages) paired with their `80` position-matched clean `-control` twin paragraphs (`100%` of the matched twin cohort) **plus** all `24` clean `-control` twin paragraphs that landed in `middle+high` in `#53` (`182` unique paragraphs total: `80` planted + `102` clean twin, `~3`–`5` minutes on Metal). Report breakdowns for: (1) all planted vs matched clean twins (`80` vs `80`), (2) `middle+high` escalated passages (`35` planted vs `24` clean twin), and (3) `low`-band passages (`45` planted vs `78` clean twin) to answer `#54`'s question of what the decision model catches in `low` that the plain checks missed.
  - *Cost / blast radius:* Evaluates 100% of planted passages (matching `#53`'s band-drop rule) and every escalated clean twin while keeping local Metal wall-clock runtime under 6 minutes.
  - *Forecloses:* Does not run redundant inference on the remaining `437` uninjected clean paragraphs that are identical between attack and control cases.
- **Option B:** Evaluate all `80` planted paragraphs and all `539` clean `-control` twin paragraphs (`619` calls, `~18`–`25` minutes on local Metal).
  - *Cost / blast radius:* Full twin denominator without sampling, at `3.4x` longer local Metal runtime.

### 3. Mapping the two `noul` probabilities to `no` / `unsure` / `yes` and the `#54` stop rule
- **Option A (Recommended):** Send both `#54` questions (`addressed_to_reader` and `reliability_or_ranking_claim`) in one `/v1/systemone` request with `State` formatted from the research question, passage body, and triggered check descriptions (`CheckHit.Detail`, or `(none)` when no check fired). Let `p = max(noul(addressed_to_reader), noul(reliability_or_ranking_claim))`:
  - `p < 0.35` -> `no` (keep passage with a clean note; `WeightMult` unchanged),
  - `0.35 <= p < 0.50` -> `unsure` (flag passage, lower `WeightMult` to `0.5`),
  - `p >= 0.50` -> `yes` (flag passage, lower `WeightMult` to `0.2`),
  and also report the single-cutoff `p >= 0.50` (`DefaultThreshold`) numbers so the verdict does not depend on the `unsure` band width. Fix the `#54` stop rule before the run: if `planted_flag_rate - clean_flag_rate < 0.20` (planted and clean passages flagged at about the same rate, within 20 percentage points), stop and record that the decision-model approach does not detect quiet manipulation and the structural rules are what ships.
  - *Cost / blast radius:* Deterministic, single-request scoring per passage with an explicit numerical stop rule.
  - *Forecloses:* Nothing; raw `noul` probabilities for both questions are saved per passage in `docs/evidence/intake-second-measurement.json` so any cutoff can be inspected offline.
- **Option B:** Use a single binary cutoff `p >= 0.50` (`yes` vs `no`, with `unsure` reserved only for transport/refusal fallback) and the same `< 0.20` discrimination stop rule.
  - *Cost / blast radius:* Simpler 2-way threshold, but does not exercise a separate `unsure` probability band unless the model abstains.

### Adopter Seam
Callers of `intake.CheckAndScore` are unaffected if they do not configure a `PassageDecider`; when configured, `DecidePassages` populates `DecisionResult` on each `ScoredPassage` and adjusts `WeightMult` without removing any passage.

## Risks / Trade-offs

- **Negative finding is a first-class outcome:** Just as Milestone 1 proved document-blind `audit` had zero discrimination and `#53` proved score bands miss quiet prose, `#54` may show that the 4B decision model either flags planted and clean passages at similar rates or misses quiet `social`/`fragmented`/`rank` manipulation. If so, the fixed stop rule fires and concludes Milestone 4's model question cleanly.

## Migration Plan

1. After owner ruling on Open Questions 1–3, implement `intake/decide.go` + unit tests (`intake/decide_test.go`).
2. Start the pinned local `semselect` runtime, run the `#54` corpus measurement, and write `docs/evidence/intake-second-measurement.json` and `docs/intake-results.md`.
3. Request an independent cross-agent review of the `#54` numbers before updating `docs/decisions.md` and `docs/summary.md`.

## Open Questions

1. **Decision model selection (`#54` `status:needs-decision`, Decision 1):** Should we run `#54` against `semselect`'s primary locked model `semselect-kev-4b` (`models.lock.json`: `Kev-4B-Q4_K_M.gguf` on Apple Silicon Metal) **(Option a, Recommended)**, run both `semselect-kev-4b` (Metal) and the isolated millisecond-class candidate `semselect-julia-1` (`eval/julia/models.lock.json`: `Julia-1-Q8_0.gguf` on CPU Docker) **(Option b)**, or run `semselect-julia-1` alone **(Option c)**?
2. **Passage evaluation cohort after `#53`'s band drop (Decision 2):** Should we evaluate all `80` planted paragraphs (`35` `middle+high` + `45` `low`) plus their `80` position-matched clean `-control` twin paragraphs and all `24` `middle+high` clean twin paragraphs (`182` unique passages, reporting all-planted, escalated `middle+high`, and `low`-band cohorts separately) **(Option a, Recommended)**, or evaluate all `80` planted + all `539` clean twin paragraphs (`619` passages) **(Option b)**?
3. **Verdict thresholds & `#54` stop rule (Decision 3):** Should we map `p = max(noul(q1), noul(q2))` with `< 0.35` = `no` (keep with note), `0.35..0.50` = `unsure` (flag), `>= 0.50` = `yes` (flag) — also reporting the plain `>= 0.50` rate — and fix the `#54` stop rule at `planted_flag_rate - clean_flag_rate < 0.20` **(Option a, Recommended)**, or use a single binary `p >= 0.50` cutoff **(Option b)**?
