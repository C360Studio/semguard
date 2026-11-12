# reader Specification

## Purpose
The single OpenAI-compatible chat completion client for the reader stage: ctx-honoring, bounded request and response bodies, free and strict JSON-schema modes, no retries, and no tools. Design: `openspec/changes/archive/2026-11-11-reader-verify/design.md`.
## Requirements
### Requirement: Single OpenAI-compatible chat completion

`Client.Complete` SHALL send one `POST` request to `<BaseURL>/chat/completions` honoring `ctx`, with no automatic retries and no tools unless schema fallback is configured. It SHALL bound the response body to `MaxResponseBytes` (default 1 MiB) and return an error on transport failure, non-2xx status, oversized response, or malformed envelope.

#### Scenario: reader-complete-free

- **WHEN** `Complete` is called in `free` mode with `System` and `User` messages
- **THEN** the request payload carries those two messages without `response_format` or `tools`, and returns the assistant message content

#### Scenario: reader-complete-schema

- **WHEN** `Complete` is called in `schema` mode with a task schema
- **THEN** the request payload sets `response_format` to strict `json_schema` with the task's schema, and the response records `SchemaSHA256`

#### Scenario: reader-refusal

- **WHEN** the endpoint returns a completion with a non-empty `refusal` field or empty content with refusal finish reason
- **THEN** `Complete` returns `Refused: true` (B2) without returning a transport error

#### Scenario: reader-bounded-response

- **WHEN** the endpoint streams a response body exceeding `MaxResponseBytes`
- **THEN** `Complete` returns an error and does not buffer unbounded bytes

### Requirement: Transient error classification

`Client.Complete` SHALL return a non-2xx response as a `*StatusError` carrying the code, and `Transient` SHALL report
true only for a request timeout, a connection error, HTTP 429, or HTTP 5xx, so a caller can retry those and nothing
else. `Complete` itself SHALL make one attempt.

#### Scenario: reader-transient-errors

- **WHEN** `Complete` fails with HTTP 429, 500, or 503, a refused connection, or a request timeout
- **THEN** `Transient` reports true; for HTTP 400, 401, 404, or a malformed 200 body it reports false
