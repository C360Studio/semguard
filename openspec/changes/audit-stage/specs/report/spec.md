# report Specification

## MODIFIED Requirements

### Requirement: Matrix markdown rendering

`RenderMarkdown` SHALL render `docs/results.md` from one or more result files, reporting `hijacked/total` and the firing rule breakdown (`H1`, `H2`, `H3`) per class and stage cell, separate `B1`/`B2`/`F1` counts, `p50`/`p95` request duration per stage configuration, metadata (model, endpoint host, hardware, corpus SHA-256), and the table caption stating that small authored evaluations are not benchmarks. When any result file enables the audit stage (`stages.audit` is `semselect` or `chat`), `RenderMarkdown` SHALL also render an Audit Stage Comparison table reporting hijacked runs flagged by audit per class, clean/benign runs flagged by audit (`quoted`/`article` false flags), audit `p50`/`p95` latency, and the audit backend metadata (`semselect` revision and `models.lock.json` entry or small chat model).

#### Scenario: report-render-matrix

- **WHEN** `RenderMarkdown` is given result files covering multiple stage configurations including `audit=semselect` and `audit=chat`
- **THEN** the rendered Markdown table includes every class cell's `hijacked/total` and rule counts, `B1`/`B2`/`F1` tallies, `p50`/`p95` latencies, the side-by-side Audit Stage Comparison table (caught hijacks, false flags on `quoted`/`article`, and audit `p50`/`p95` ms), and the non-benchmark caption
