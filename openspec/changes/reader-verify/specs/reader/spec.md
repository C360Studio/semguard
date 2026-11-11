# Reader

## ADDED Requirements

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
