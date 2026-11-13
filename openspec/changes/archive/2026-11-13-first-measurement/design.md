# Design: First Measurement — Do the Plain Checks Make Planted Passages Stand Out? (#53)

## Context

- `internal/corpus/corpus.go:55-67,100,138-185`: `corpus.LoadWithHash("corpus")` verifies `corpus/corpus.lock.json` (`sha256:0ee5145661e4...`) and loads `140` `Case` entries (`60` attack cases across `override`, `exfil`, `social`, `fragmented`, `framing`, `rank`; `60` `-control` clean twins; `20` benign `quoted` and `article` cases). Each `Case` has `3` `Document{ID, Text}` items (`558`–`1,719` bytes each, `2`–`6` paragraphs separated by `"\n\n"` via `corpus.Paragraphs(text)` at `corpus.go:100`, with each paragraph `60`–`572` bytes and no Markdown `#` headings). Ground truth on every attack case is `InjectedDoc` (`"d1"`..`"d3"`) and 1-based `InjectedParagraphs` (`[]int`).
- `intake/split.go:28-30,81-98,350-352`: `DefaultPassageBounds` is `PassageBounds{Ceiling: 2000, Floor: 400, HardMax: 6000}`. In `subdivide` (`split.go:350`), any headingless section with `end - start <= Ceiling` is returned as a single passage. Because every document in `corpus/cases.json` is `<= 1,719` bytes and has no Markdown headings, `Split` with `DefaultPassageBounds` keeps each 3-paragraph document as **one whole-document passage**, merging the planted paragraph with the 2–3 clean paragraphs in `injected_doc`.
- `intake/checks.go:126-151` (`checkUnlistedLink`): fires only when `rep.LinksPresent` is `true` and a passage contains a URL absent from `rep.Links` (which comes from `results[].extras.links` in `intake/exa.go:309-334`).
- `intake/score.go:18-36,85-195`: `CheckAndScore` evaluates all 8 checks with `DefaultWeights` (`talks_to_reader: 3`, `self_rank_claim: 2`, `fake_markers: 3`, `encoded_blob: 3`, `unlisted_link: 2`, `lone_site: 1`, `shared_origin: 1`, `off_topic: 1`) and cut-offs `DefaultMiddleCutoff = 2`, `DefaultHighCutoff = 5`.

## Goals / Non-Goals

- **Goals:**
  1. Mechanically convert each `corpus.Case` (attack and `-control` twin, plus benign `quoted`/`article`) into a `servicesim` Exa scenario and run it through the Exa intake path in-process with zero model calls and zero agent reading of case text.
  2. Map ground-truth planted paragraphs (`injected_doc`, `injected_paragraphs`) to passages cleanly so every planted passage and every clean passage is scored against the exact oracle.
  3. Report per attack class (with `rank` also reported separately) and per check how many planted and clean passages triggered each finding, report per band (`low`, `middle`, `high`) how many planted and clean passages landed in each band (initial + at most one tuned pass clearly marked as tuned), and apply the fixed stop rule (`>= 80%` planted in `middle+high` and `<= 10%` clean in `middle+high`).
- **Non-Goals:**
  - Calling any LLM or `semselect` endpoint (`#54`), making live network requests (`#55`), or writing conclusions into `docs/decisions.md` / `docs/summary.md` before the independent review of the numbers.

## Decisions

### 1. Passage granularity for `corpus/cases.json` documents (where every document is `< 2,000` bytes and headingless)
- **Option A (Recommended):** Split on `\n\n` paragraph boundaries when evaluating multi-paragraph headingless documents (or add `SplitParagraphs` / paragraph-boundary mode in `intake`), alongside reporting the `DefaultPassageBounds` (`Ceiling: 2000`) whole-document result for comparison. Because `corpus/cases.json` plants attacks at 1-based `\n\n` paragraph indices (`injected_paragraphs`) inside short `558`–`1,719`-byte documents without headings, paragraph-level passages map 1-to-1 (`80` planted passages across the `60` attack cases, including `3` non-adjacent planted paragraphs in each `fragmented` case) without merging clean and planted paragraphs into one passage.
  - *Cost / blast radius:* Tiny helper in `intake` or the measurement harness; `DefaultPassageBounds` remains unchanged for long web documents.
  - *Forecloses:* Nothing.
- **Option B:** Run only `DefaultPassageBounds` (`Ceiling: 2000`), treating a whole-document passage as "planted" whenever the document is `injected_doc`.
  - *Cost / blast radius:* Zero splitter options, but collapses each document into 1 passage (`60` document-passages instead of `80` planted paragraphs) and hides the clean paragraphs inside `injected_doc`.

### 2. Synthesizing `servicesim` source URLs and `extras.links` in the mechanical converter
- **Option A (Recommended):** Assign each document `d` in case `c` a deterministic reserved URL on a distinct site per document (`https://doc-<d.ID>.<familyID>.example/report`, where `<familyID>` is `c.ID` stripped of `-control` so an attack case and its `-control` twin share identical document URLs and sites), and populate `extras.links` on each result from the canonical URLs present in the **clean twin** of that document (for a control or benign case, the document's own URLs; for an attack case, the URLs in its `-control` twin document). This models a realistic page crawl where legitimate links in the page template/body are in the crawler's link list while an attacker-injected exfiltration URL in a planted paragraph is not.
  - *Cost / blast radius:* Mechanical lookup from the paired `-control` case in `cases.json`.
  - *Forecloses:* Nothing; also allows reporting what happens when `extras.links` is omitted.
- **Option B:** Leave `extras.links` empty or copy all URLs from the attacked document itself into `extras.links`.
  - *Cost / blast radius:* Copying attacked URLs into `extras.links` makes `unlisted_link` impossible to fire (`0/80`); leaving `extras.links: []` flags every legitimate URL in clean paragraphs as `unlisted_link`.

### 3. Clean-passage denominator and where the measurement report lives
- **Option A (Recommended):** Report clean passage counts both for (a) the `60` `-control` twin scenarios (`539` clean paragraph-passages, the direct twin comparison for the `60` attack scenarios) and (b) all clean passages across the full 140-case run (clean paragraphs in attack cases, `-control` twins, and benign `quoted`/`article` cases, broken down by class), and write the reproducible JSON + Markdown tables to `docs/evidence/intake-first-measurement.json` and `docs/intake-results.md` via a `go test` / CLI runner before independent review.
  - *Cost / blast radius:* Self-contained evidence file and Markdown report; `docs/decisions.md` and `docs/summary.md` stay untouched until after the independent read required by `#53`.
- **Option B:** Report only inside `go test -v` logs without a committed Markdown/JSON evidence artifact.
  - *Cost / blast radius:* Harder for the independent reviewer and owner to inspect side-by-side.

### Adopter Seam
External consumers of `github.com/c360studio/semguard/intake` are unaffected if they do nothing; `internal/intakemeasure` is internal to the lab harness and `docs/intake-results.md` records the first M4 measurement.

## Risks / Trade-offs

- **Stop-rule outcome:** If neither the initial weights/cut-offs nor a single tuned weight/cut-off configuration achieves `>= 80%` planted in `middle+high` with `<= 10%` clean in `middle+high` (because `social`, `fragmented`, and half of `rank` use quiet prose with no overt markers), the fixed stop rule fires and drops the bands for `#54`. Reporting that honestly is the core scientific purpose of `#53`.

## Migration Plan

1. Implement `internal/intakemeasure` and generate `docs/evidence/intake-first-measurement.json` and `docs/intake-results.md`.
2. Obtain the independent review on the PR before updating `docs/decisions.md` or `docs/summary.md`.

## Open Questions

1. **Passage granularity for the corpus measurement (Decision 1):** Should the measurement evaluate paragraph-level passages (`1` passage per `\n\n` paragraph, matching `injected_paragraphs` 1-to-1) alongside reporting the `DefaultPassageBounds` (`Ceiling: 2000`) whole-document numbers **(Option a, Recommended)**, or evaluate only `DefaultPassageBounds` whole-document passages **(Option b)**?
2. **Mechanical converter `extras.links` rule (Decision 2):** Should the converter populate each document's `extras.links` from the clean twin document's URLs and assign each document a distinct site host shared between the attack case and its `-control` twin **(Option a, Recommended)**, or omit `extras.links` **(Option b)**?
3. **Report artifact & independent-read workflow (Decision 3):** Should this PR commit the measurement runner plus `docs/evidence/intake-first-measurement.json` and `docs/intake-results.md` (with both initial and single-tuned tables and the stop-rule result) for the independent reviewer to read before `docs/decisions.md` / `docs/summary.md` are updated **(Option a, Recommended)**, or keep the numbers in test output only **(Option b)**?
