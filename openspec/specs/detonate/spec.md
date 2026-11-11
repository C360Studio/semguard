# detonate Specification

## Purpose
The detonation runner CLI (`cmd/detonate`): executes the frozen corpus across frame (`datamark`, `framing`) and verify (`free`, `schema`) configurations against the OpenAI-compatible reader endpoint, writes results JSON files, and invokes matrix rendering. Design: `openspec/changes/archive/2026-11-11-detonate-report/design.md`.
## Requirements
### Requirement: Stage configuration and corpus execution

`cmd/detonate` SHALL load and hash-verify the corpus (`--cases`), execute optional warmup requests (`--warmup`), and run every selected case against the OpenAI-compatible reader endpoint with bounded concurrency (`--concurrency`, default 2), per-request timeout (`--timeout`), zero retries, and the requested stage switches (`--frame`, `--datamark`, `--verify free|schema`, `--audit off|semselect|chat`). When `--audit` is `semselect` or `chat`, `cmd/detonate` SHALL invoke the configured audit scorer on each non-refused, non-broken run and record audit outcomes, latency, and audit backend provenance (`semselect` revision and `models.lock.json` entry, or chat model ID). It SHALL write the complete results JSON to `--out` in deterministic corpus order.

#### Scenario: detonate-stage-wiring

- **WHEN** `detonate` runs across the frame (`datamark`, `framing`), verify (`free`, `schema`), and audit (`off`, `semselect`, `chat`) combinations
- **THEN** each run sends the corresponding framed prompt, schema format, and optional audit probe, and records the stage switches, prompt/framing SHA-256, schema SHA-256, corpus SHA-256, model, audit provenance, endpoint host, hardware, duration, and rule/audit outcomes in the output file

#### Scenario: detonate-hijack-exits-zero

- **WHEN** a completion triggers H1, H2, H3, B1, B2, F1, or an audit flag without transport errors
- **THEN** `detonate` records the outcome in the results file and exits with code 0

#### Scenario: detonate-transport-error-exits-nonzero

- **WHEN** any reader or audit request fails with a transport error, timeout, or malformed HTTP envelope
- **THEN** `detonate` fails with a nonzero exit code

