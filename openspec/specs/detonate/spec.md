# detonate Specification

## Purpose
The detonation runner CLI (`cmd/detonate`): executes the frozen corpus across frame (`datamark`, `framing`) and verify (`free`, `schema`) configurations against the OpenAI-compatible reader endpoint, writes results JSON files, and invokes matrix rendering. Design: `openspec/changes/archive/2026-11-11-detonate-report/design.md`.
## Requirements
### Requirement: Stage configuration and corpus execution

`cmd/detonate` SHALL load and hash-verify the corpus (`--cases`), execute optional warmup requests (`--warmup`), and run every selected case against the OpenAI-compatible reader endpoint with bounded concurrency (`--concurrency`, default 2), per-request timeout (`--timeout`), up to 3 attempts per case for transient reader errors (`reader.Transient`: request timeout, connection error, HTTP 429, HTTP 5xx) with a context-honoring wait between attempts and the attempt count recorded on each run, no retry on any other error, `--trials` runs of the corpus (default 1), and the requested stage switches (`--frame`, `--datamark`, `--verify free|schema`, `--audit off|semselect|chat`). When `--audit` is `semselect` or `chat`, `cmd/detonate` SHALL invoke the configured audit scorer on each non-refused, non-broken run and record audit outcomes, latency, and audit backend provenance (`semselect` revision and `models.lock.json` entry, or chat model ID). It SHALL write the complete results JSON to `--out` in deterministic corpus order.

#### Scenario: detonate-stage-wiring

- **WHEN** `detonate` runs across the frame (`datamark`, `framing`), verify (`free`, `schema`), and audit (`off`, `semselect`, `chat`) combinations
- **THEN** each run sends the corresponding framed prompt, schema format, and optional audit probe, and records the stage switches, prompt/framing SHA-256, schema SHA-256, corpus SHA-256, model, audit provenance, endpoint host, hardware, duration, and rule/audit outcomes in the output file

#### Scenario: detonate-hijack-exits-zero

- **WHEN** a completion triggers H1, H2, H3, B1, B2, F1, or an audit flag without transport errors
- **THEN** `detonate` records the outcome in the results file and exits with code 0

#### Scenario: detonate-transport-error-exits-nonzero

- **WHEN** any reader or audit request fails with a transport error, timeout, or malformed HTTP envelope
- **THEN** `detonate` fails with a nonzero exit code

#### Scenario: detonate-trials

- **WHEN** `detonate` runs with `--trials 3`
- **THEN** it sends every case once per trial, writes the runs trial-major in corpus order with a 1-based `trial` on each, records `trials: 3` in the results file, and evaluates each trial's `rank` cases against that trial's control twins; `--trials` outside 1..10 exits 2 before any request

#### Scenario: detonate-transient-retry

- **WHEN** a reader call fails with a timeout, connection error, HTTP 429, or HTTP 5xx and a later attempt within 3 succeeds
- **THEN** `detonate` records the successful run with its `attempts` and the successful attempt's `duration_ms`; an error that persists through 3 attempts, any other 4xx, or a malformed 2xx body still fails the command with a nonzero exit and no output file

### Requirement: Rescore stored evidence

`detonate rescore` SHALL re-evaluate the stored raw responses of each given results file in place with the current
`report.Evaluate` and `verify` code, making no reader calls, and SHALL keep every stored measurement field (`trial`,
`attempts`, `duration_ms`, prompts, raw responses, file metadata). It SHALL refuse, without modifying it, a file whose
`corpus_sha256` differs from the current corpus or that carries audit outcomes.

#### Scenario: detonate-rescore

- **WHEN** a stored run was scored `H2` under an older URL pattern that counted a markdown backtick as part of a URL
- **THEN** `detonate rescore` rewrites it without `H2`, keeps `trial`, `attempts`, and `duration_ms`, and refuses a
  file from another corpus without changing it
