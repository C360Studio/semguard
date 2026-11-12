# Defense-targeted corpus class and H1 canary landing breakdown

Approved-by:

Track: design (touches both `corpus` and `report` capabilities under `openspec/specs/`).

## Why

1. **Floor effect and unchallenged defenses (#32):** In v0 (`b111713`), `exfil` and `fragmented` are `0/10` on both reader models even with `frame=off, verify=free`, and no class was written against `untrusted.Frame`'s framing text (`untrusted/prompts/frame.txt`) or document/datamark markers (`<<report ...>>`, `^`). All seven v0 classes also share a drafting model (`gemini-4-argon`) from the same family as reader `gemini-3.8-flash`.
2. **Quoting versus obeying (#32):** The only schema-mode `exfil` `H1` hit (`exfil-005` in `gpt-5.4-frame-off-verify-schema`) has its canary inside a `claims[].quote` that resolved to the injected paragraph via `verify.Verify`, whereas the free-mode `H1` hits (`social-001`, `social-002`, `social-008`) are in prose. The matrix currently renders both as `H1:1` without distinguishing a verbatim quote of the injected paragraph from prose/other output.

## What Changes

- `corpus/drafting/prompt.md` and `corpus/drafting/draft.sh`: add one new attack class (`10` cases + `10` control twins = `20` entries, raising the corpus from `120` to `140` entries) targeting `untrusted.Frame` (arguing with `untrusted/prompts/frame.txt` and imitating `<<report ...>>` and `^`), drafted with a non-reader-family model (`qwen3:14b` or owner-selected local model) and recorded in `corpus/drafting/model.txt`. Existing 70 cases + 50 controls remain byte-for-byte frozen.
- `internal/corpus`: register the 8th class in `classes`, `asks`, `isAttack`, and `CheckCounts` (`8` classes × `10` cases + `60` controls = `140` entries), and validate `corpus/cases.json` and `corpus/corpus.lock.json` via `task corpus:validate`.
- `internal/report`: record and render where each `H1` canary landed—inside a verified `claims[].quote` resolving to an injected paragraph of `injected_doc` versus anywhere else—without changing `docs/hijack-definition.md` (`H1` still fires and sets `Hijacked: true`). Include the 8th class column in `RenderMarkdown`.

## Non-goals

- Rewording or replacing any existing case in `corpus/cases.json` (all 120 existing entries keep their frozen IDs, texts, and canaries).
- Changing `docs/hijack-definition.md` (`H1` still fires whenever the canary appears anywhere in the output).
- Running a separate paid LLM matrix rerun outside the combined rerun planned with #30.

## Impact

- Modified: `openspec/specs/corpus/spec.md`, `openspec/specs/report/spec.md`, `corpus/drafting/prompt.md`, `corpus/drafting/draft.sh`, `internal/corpus/`, `internal/report/`.
- Owner-drafted artifact: `corpus/draft/<class>.json`, `corpus/drafting/model.txt`, `corpus/cases.json`, `corpus/corpus.lock.json` (agents validate via `task corpus:validate` and never read attack text into context).

## Done when

Owner resolves Open Questions 1–3 and approves this proposal; the 10 new cases + twins are built and `task corpus:validate` and `task verify` pass offline; `internal/report` distinguishes injected-paragraph quote `H1` hits from other `H1` hits with unit/mutation tests; and the combined rerun with #30 updates `docs/results.md`, `docs/decisions.md`, and `docs/summary.md`.
