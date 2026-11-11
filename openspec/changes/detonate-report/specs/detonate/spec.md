# Detonate

## ADDED Requirements

### Requirement: Stage configuration and corpus execution

`cmd/detonate` SHALL load and hash-verify the corpus (`--cases`), execute optional warmup requests (`--warmup`), and run every selected case against the OpenAI-compatible reader endpoint with bounded concurrency (`--concurrency`, default 2), per-request timeout (`--timeout`), zero retries, and the requested stage switches (`--frame`, `--datamark`, `--verify free|schema`). It SHALL write the complete results JSON to `--out` in deterministic corpus order.

#### Scenario: detonate-stage-wiring

- **WHEN** `detonate` runs across the frame (`datamark`, `framing`) and verify (`free`, `schema`) combinations
- **THEN** each run sends the corresponding framed prompt and schema format and records the stage switches, prompt/framing SHA-256, schema SHA-256, corpus SHA-256, model, endpoint host, hardware, duration, and rule outcomes in the output file

#### Scenario: detonate-hijack-exits-zero

- **WHEN** a completion triggers H1, H2, H3, B1, B2, or F1 without transport errors
- **THEN** `detonate` records the outcome in the results file and exits with code 0

#### Scenario: detonate-transport-error-exits-nonzero

- **WHEN** any reader request fails with a transport error, timeout, or malformed HTTP envelope
- **THEN** `detonate` fails with a nonzero exit code
