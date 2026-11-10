# semguard

A lab that measures which defenses stop an injected document from hijacking the LLM step of an OSINT pipeline, and at
what cost. It is not a product. Its consumer is havocnow.

Input classifiers do not predict hijack well on the classes OSINT evidence carries (exfiltration links, social
engineering, fragmented attacks). semguard measures the structural defenses instead (datamarked untrusted input,
schema-only judge output, a judge with no tools) against a frozen, hash-pinned detonation corpus, and publishes the
hijack rate per attack class, judge model, and defense configuration in `docs/results.md`.

The corpus is small and hand-authored. Its numbers are a lab measurement, not a benchmark.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/detonate` | corpus -> fake provider -> judge -> hijack report (milestone 1) |
| `cmd/guard` | classify service (milestone 2, only if milestone 1 shows a gap) |
| `internal/untrusted` | segment, strip, datamark, allowlist (exported as a module in milestone 3) |
| `internal/judge` | output schema, validator, prompt builder, the one LLM client |
| `internal/corpus` | case loader, hash verification, canaries |
| `internal/report` | results schema and `docs/results.md` rendering |
| `corpus/` | `cases.json` and `corpus.lock.json` |
| `docs/` | `hijack-definition.md` (the oracle), `results.md`, `decisions.md`, `evidence/` |

## Use

```bash
cp .env.example .env   # set the OpenAI-compatible endpoint, key, and judge model
task check             # offline tests and lint; establishes nothing about models
```

Requires Go (version in `go.mod`), [Task](https://taskfile.dev), and golangci-lint v2. The agent workflow is in
`AGENTS.md`.

## License

MIT. See `LICENSE` and `THIRD_PARTY_NOTICES.md`.
