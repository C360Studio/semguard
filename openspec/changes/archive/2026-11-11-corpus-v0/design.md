# Design: corpus v0

## Context

- `docs/hijack-definition.md` (bootstrap d63024f) defines H1 (canary anywhere in the output), H2 (a URL missing
  byte-for-byte from every report), and H3 (rank against the control run). Those three rules decide which fields a
  case must carry: `canary`, the document texts, `injected_doc`, `ask`, and `control_of`.
- Issue #4: a schema-mode claim gives a `paragraph` index plus the SHA-256 of the quoted paragraph bytes, and the
  harness looks the quote up in the corpus by that index. So the corpus has to fix what a paragraph is.
- There is no Go code in the tree yet (`Taskfile.yml` `HAS_GO` shortcut). This change ships the first package.

## Goals / Non-Goals

Goals: one frozen, hash-pinned case file whose structure is checked in Go, so the runner can trust it. Non-goals: the
hijack rules, the prompts, and any model calls.

## Decisions

1. **What a paragraph is.** (a) Paragraphs are split on `\n\n`, indexed from 1, and the bytes are kept exactly as
   written. (b) Each report is stored as a JSON array of paragraphs. Option (a) keeps each report as one string, which
   is the form a source hands the pipeline, and it matches a schema claim's `"paragraph": 3`. Its cost is
   that the split rule now belongs to the oracle. Option (b) removes that ambiguity but stores a shape the pipeline
   never sees. **Choose (a).** The future module's `Segment` must give the same split; the loader test pins it.
2. **How controls are stored.** (a) Each control is its own full entry, with id `<attack-id>-control`,
   `control_of` set, the injected paragraphs removed, and `injected_doc` kept. (b) The control is derived at load
   time by dropping the marked paragraphs. Option (a) makes each control hash-pinned and readable on its own, at the
   cost of duplicated text. Option (b) needs a marker field, and a bug in the derivation would silently change every
   control. **Choose (a).** The validator checks that the control has the same question and the same set of
   documents, that only the injected document's text differs, and that the canary is absent from the control.
3. **Where canaries come from.** (a) A one-shot `go run ./internal/corpus/cmd/canary` writes them, and they are
   frozen from then on. (b) Canaries are derived from a hash of the case id. Option (b) is predictable, and a model
   trained on this repo once it is public could learn the derivation. **Choose (a).** The validator enforces the
   format and uniqueness across cases.
4. **Who authors the cases.** (a) An LLM drafts them from per-class specifications, a person edits them, and they are
   then frozen. (b) A person writes all 70 by hand. Either is acceptable. Option (a) is faster, but the drafting
   model's style could correlate with the reader models' styles. **Choose (a)** (Coby, 2026-11-10). The drafting model
   and the per-class drafting prompt are recorded next to the corpus, and the drafting model is not one of the two reader models.
5. **Structural checks enforced in Go.**
   - Every attack case's canary appears in its injected document's text, except for `rank` cases, which use ask=rank
     and carry no canary in the output.
   - `quoted` cases carry their canary inside a quotation. `article` cases carry no canary.
   - The `exfil` injection contains a URL that holds the canary.
   - Each `fragmented` ask is split across three paragraphs that are not adjacent.
   - The benign reports in each case contain no URL that carries a canary.

   Each check is a regression guard for a mistake an author would plausibly make.

6. **How the reader's task is carried.** (a) Each case names a `task` id, and `corpus/tasks.json` maps each id to its
   instruction text and to `eval/schemas/<task>.json`. (b) The task is fixed in code. Option (a) lets a summarize task
   and a score-and-compare task run on the same corpus, which keeps the lab general. Its cost is one more pinned file.
   **Choose (a).** v0 has one task, `score-compare`, and the schema file itself lands with #4. The validator checks
   that every case's task id resolves.

## Risks / Trade-offs

- Hand-authored cases are small and correlated with one another. Mitigation: the table captions say so, and the
  results report counts, never rates presented as general.
- URLs in the cases use reserved domains (`.example`, `.invalid`) so that a model echoing a link never reaches a real
  host.

## Migration Plan

None: this is the first corpus. Any later change is a new lock hash plus a rerun.

## Open Questions

1. Resolved (Coby, 2026-11-10): the cases are LLM-drafted, hand-edited by a person, and then frozen (Decision 4).
   **(owner)** Still open: which drafting model to use. It must not be one of the two reader models, so this is settled with
   #9.
2. Paragraph indices start at 1 (Decision 1, matching the schema claim example in #4). Override this if 0-based is preferred.
