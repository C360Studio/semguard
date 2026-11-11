# Design: segment, strip, and export the module API

## Context

- Issue #7 requires promoting `internal/untrusted` and `internal/verify` to `github.com/c360studio/semguard/untrusted` and `github.com/c360studio/semguard/verify` with `Segment(text) []Paragraph`, `Strip(text) (string, OffsetMap)`, `Datamark(docs) Block`, `Frame(task, docs) Prompt`, `Verify(raw, docs) (Record, []Violation)`, and `AllowedLinks(output, docs) []Violation`, plus tagging a release.
- `internal/untrusted/untrusted.go:13-38,59-98` defines `Document`, `Task`, `Options`, `Prompt`, `check(d)` (rejecting `<<report`, `<</report`, and `^`), `Datamark(docs) (string, error)`, `Unmark(s) string`, and `Frame(t, docs, o) (Prompt, error)`.
- `openspec/changes/archive/2026-11-11-untrusted-frame/design.md:46-48` (Decision 4) noted that `Datamark` refuses documents containing `<<report`, `<</report>>`, or `^` until #7 (`Strip`).
- `openspec/changes/archive/2026-11-11-corpus-v0/design.md:19-23` (Decision 1) requires `Segment` to produce the exact same `\n\n` 1-based paragraph split as `internal/corpus/corpus.go:100` (`Paragraphs(text)`).
- `internal/verify/verify.go:30-65,101,364,400` defines `Document`, `Score`, `Claim`, `Record`, `Violation`, `Verify`, `AllowedLinks`, and `ParseFree`, with zero internal imports (`verify.go:5-16`).
- `Taskfile.yml:22` runs `gofmt -l cmd internal`; moving packages to top-level `untrusted/` and `verify/` requires adding `untrusted verify` (or `.`) to the `gofmt` check.
- `cmd/detonate/main.go:26` imports `internal/untrusted` and `internal/report/report.go:17` imports `internal/verify`.

## Goals / Non-Goals

Goals: promote `untrusted` and `verify` to clean top-level module packages, add `Segment` and `Strip` with `Paragraph` and `OffsetMap`, and keep all existing callers and tests green. Non-goals: gate client (#10), audit stage (#14), or consumer-specific wrappers.

## Decisions

1. **What `Strip(text) (string, OffsetMap)` removes or normalizes.**
   - **(a) Strip invisible/control characters (C0/C1 controls except `\n` and `\t`, plus Unicode format/zero-width runes like `U+200B..U+200F`, `U+202A..U+202E`, `U+2060..U+206F`, `U+FEFF`), normalize `\r\n` and `\r` to `\n`, and strip the reserved datamark tokens (`^`, `<<report`, `<</report`) so any stripped text is guaranteed to pass `Datamark`/`Frame` marker checks.**
   - **(b) Strip only invisible/zero-width Unicode format characters and control characters (leaving `^`, `<<report`, and `<</report` to fail `Datamark` with an error).**
   - *Trade-off:* Under (a), an adopter who runs `Strip` before `Frame`/`Datamark` never hits a runtime marker-spoofing error on real-world documents containing a literal `^` (e.g. math/markdown) or `<<report`, fulfilling #5 Decision 4's deferral to #7. Under (b), `Strip` only removes invisible Unicode/control characters and documents with `^` still error in `Datamark`. **Proposed: (a); see Open Question 1.**
   - *Seam:* `Strip` returns `(cleaned string, OffsetMap)` where `OffsetMap.OriginalOffset(cleanIdx int) int` maps any byte offset `0 <= cleanIdx <= len(cleaned)` back to its exact source byte offset in `text` (`cleanText[i]` originated at `text[m.OriginalOffset(i)]`).

2. **`Paragraph` and `OffsetMap` types in `untrusted`.**
   - **(a) `Paragraph{Index int, Text string, Start int, End int}` (1-based `Index`, byte slice `text[Start:End] == Text`, splitting on `\n\n` exactly like `corpus.Paragraphs`) and `OffsetMap` with `OriginalOffset(cleanOffset int) int` and `Span(cleanStart, cleanEnd int) (origStart, origEnd int)`.**
   - **(b) `Paragraph` with only `{Index int, Text string}` and `OffsetMap` as a raw `[]int` slice.**
   - *Trade-off:* Option (a) gives adopters both 1-based paragraph index and byte spans (`Start`, `End`) that compose directly with `OffsetMap.Span(p.Start, p.End)` to locate a paragraph or verified quote in the original unstripped document. **Proposed: (a); see Open Question 2.**
   - *Seam:* `Segment(text)` always returns at least 1 paragraph (matching `strings.Split(text, "\n\n")`), and for every `p` in `Segment(text)`, `p.Index == i+1` and `text[p.Start:p.End] == p.Text`.

3. **`Block` return type of `Datamark` and package move layout.**
   - **(a) Define `type Block = string` in `untrusted` (`Datamark(docs []Document) (Block, error)`), move `internal/untrusted` → `untrusted` and `internal/verify` → `verify` directly (updating `cmd/detonate` and `internal/report` imports, with no duplicate `internal/` wrappers).**
   - **(b) Define `type Block string` (distinct named string type requiring `string(b)` casts) and keep `internal/untrusted` / `internal/verify` as type-forwarding shims.**
   - *Trade-off:* `type Block = string` satisfies `untrusted.Block` in issue #7 without breaking existing string callers or requiring casts, and moving the packages directly avoids maintaining two copies or wrapper shims in a single-module repo. **Proposed: (a); see Open Question 3.**
   - *Seam:* External consumers import `github.com/c360studio/semguard/untrusted` and `github.com/c360studio/semguard/verify`.

4. **When to tag the module release.**
   - **(a) Ship the exported packages in this PR without pushing a git tag yet, or tag `v0.1.0` after merge once Coby confirms the version.**
   - **(b) Push a `v0.1.0` tag during the PR before merge.**
   - *Trade-off:* Tagging a branch commit before squash-merge points the tag at an unmerged branch SHA rather than `main`. **Proposed: (a); see Open Question 4.**
   - *Seam:* Once merged to `main`, `git tag v0.1.0` on the `main` merge commit publishes the module version.

## Risks / Trade-offs

- `Taskfile.yml:22` runs `gofmt -l cmd internal`: must be updated to `gofmt -l cmd internal untrusted verify` so lint covers the promoted top-level packages.
- `untrusted_test.go` imports `internal/corpus` as a test helper (`package untrusted`); an external consumer running `go test github.com/c360studio/semguard/untrusted` inside the module has access to `internal/corpus`.

## Migration Plan

Move `internal/untrusted` → `untrusted` and `internal/verify` → `verify` via `git mv`, update imports in `cmd/detonate` and `internal/report`, update `Taskfile.yml` and `README.md`.

## Open Questions

1. **(owner)** Confirm Decision 1(a): `Strip` normalizes `\r\n`/`\r` → `\n`, removes C0/C1 control characters (except `\n`, `\t`) and Unicode format/zero-width characters (`Cf`), and removes reserved datamark tokens (`^`, `<<report`, `<</report`) so `Strip` output is guaranteed safe for `Datamark`/`Frame`?
2. **(owner)** Confirm Decision 2(a): `Paragraph{Index, Text, Start, End}` (1-based `Index`, byte offsets `Start`/`End` on `\n\n` split) and `OffsetMap` with `OriginalOffset(int) int` and `Span(start, end int) (int, int)`?
3. **(owner)** Confirm Decision 3(a) & 4(a): `type Block = string` with `Datamark(docs) (Block, error)`, direct move to top-level `untrusted/` and `verify/` (no `internal/` shims), and hold off on pushing a git tag until after the PR squash-merges to `main`?
