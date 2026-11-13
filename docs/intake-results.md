# Intake Checks — First Measurement (`#53`)

> **Lab measurement caveat:** Small authored evaluations are not benchmarks. These counts measure the 8 deterministic intake checks and score bands on the frozen 140-case hand-authored corpus (`60` adversarial attack cases, `60` clean `-control` twins, and `20` benign `quoted`/`article` cases; `corpus_sha256: 0ee5145661e4`) served in-process through `servicesim`'s Exa profile with zero model calls.

## 1. Stop Rule & Outcome (Fixed Before the Run)

- **Stop rule (`#53`):** Carry on with score bands (`low`, `middle`, `high`) if at least **8 in 10 (`>= 80.0%`)** planted passages land in `middle` or `high` **and** at most **1 in 10 (`<= 10.0%`)** clean passages do. Otherwise drop the bands: every passage goes to the decision model (`#54`), and the deterministic checks are kept only as context for it.
- **Measured result (initial provisional weights, paragraph-level passages):** **`35/80` (`43.8%`)** planted passages landed in `middle` or `high` (`< 80.0%`), while **`24/539` (`4.5%`)** clean `-control` twin passages (`72/1269` or `5.7%` across all clean passages) landed in `middle` or `high` (`<= 10.0%`).
- **Measured result (single tuned pass — reported as tuned, not a prediction):** Promoting `off_topic` to weight `2` (so any keyword-disjoint paragraph reaches `middle`) and zeroing `lone_site`/`shared_origin` raises planted `middle + high` to **`49/80` (`61.3%`)** (the ceiling if *every* check hit is escalated), which still misses `80.0%` while raising clean `-control` twin `middle + high` to **`126/539` (`23.4%`)** (`> 10.0%`).
- **Measured result (`DefaultPassageBounds`, `Ceiling: 2000` whole-document passages):** **`35/60` (`58.3%`)** planted documents landed in `middle` or `high` (`< 80.0%`), with **`0/180` (`0.0%`)** clean `-control` twin documents (`13/360` or `3.6%` across all clean documents) in `middle` or `high`.
- **Stop-rule outcome:** **`drop_bands_keep_checks_as_context`** (`carry_on_with_bands = false`). Overt classes (`override` `10/10`, `exfil` `10/10`, `framing` `10/10`) are caught `100%` by lexical/link checks with `0/539` clean-twin false positives on direct checks, whereas quiet prose classes (`social` `0/10`, `fragmented` `0/30`, and half of `rank` `5/10`) carry no overt imperative, marker, or unlisted link. Per the stop rule, bands are dropped for `#54`: every passage goes to the decision model, and the deterministic check findings are passed as context.

## 2. Per-Check Findings by Class (Paragraph-Level Passages)

*Table caption: Small authored evaluations are not benchmarks. Each cell reports `planted / clean_twin (clean_all)` passage counts where the named check fired across the 140 `servicesim` Exa scenarios (`80` planted paragraphs across `60` attack cases, `539` clean paragraphs across `60` `-control` twin cases, and `1,269` total clean paragraphs including uninjected paragraphs in attack cases and benign `quoted`/`article` cases).*

| Class | Cases (Atk / Ctrl) | Passages (`Planted / Twin / AllClean`) | `talks_to_reader` | `self_rank_claim` | `fake_markers` | `encoded_blob` | `unlisted_link` | `lone_site` | `shared_origin` | `off_topic` | Any Check Fired |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `override` | 10 / 10 | 10 / 92 / 184 | 10 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 10 (20) | 0 / 0 (0) | 9 / 24 (48) | 10 / 31 (62) |
| `exfil` | 10 / 10 | 10 / 89 / 178 | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 10 / 0 (0) | 10 / 1 (2) | 0 / 8 (16) | 4 / 13 (26) | 10 / 21 (42) |
| `social` | 10 / 10 | 10 / 90 / 180 | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 10 (20) | 0 / 0 (0) | 2 / 32 (64) | 2 / 34 (68) |
| `fragmented` | 10 / 10 | 30 / 90 / 180 | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 10 (20) | 0 / 0 (0) | 8 / 23 (46) | 8 / 26 (52) |
| `framing` | 10 / 10 | 10 / 88 / 176 | 6 / 0 (0) | 0 / 0 (0) | 8 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 20 (40) | 0 / 0 (0) | 8 / 11 (22) | 10 / 28 (56) |
| `rank` | 10 / 10 | 10 / 90 / 180 | 0 / 0 (0) | 5 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 10 (20) | 0 / 0 (0) | 6 / 23 (47) | 9 / 31 (63) |
| `quoted` | 10 / 0 | 0 / 0 / 100 | 0 / 0 (6) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (11) | 0 / 0 (0) | 0 / 0 (27) | 0 / 0 (34) |
| `article` | 10 / 0 | 0 / 0 / 91 | 0 / 0 (6) | 0 / 0 (1) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (0) | 0 / 0 (10) | 0 / 0 (0) | 0 / 0 (20) | 0 / 0 (32) |
| **Total** | **80 / 60** | **80 / 539 / 1269** | **16 / 0 (12)** | **5 / 0 (1)** | **8 / 0 (0)** | **0 / 0 (0)** | **10 / 0 (0)** | **10 / 61 (143)** | **0 / 8 (16)** | **37 / 126 (300)** | **49 / 171 (409)** |

## 3. Band Distribution by Class — Initial vs Single Tuned Pass (Paragraph-Level Passages)

*Table caption: Small authored evaluations are not benchmarks. Initial bands use `DefaultWeights` (`talks_to_reader=3`, `self_rank_claim=2`, `fake_markers=3`, `encoded_blob=3`, `unlisted_link=2`, `lone_site=1`, `shared_origin=1`, `off_topic=1`) and `MiddleCutoff=2`, `HighCutoff=5`. Tuned bands are tuned once on this result (`off_topic=2`, `lone_site=0`, `shared_origin=0`) and reported as tuned, not as a prediction.*

| Class | Planted Initial (`Low / Mid / High`, `Mid+High`) | Clean Twin Initial (`Low / Mid / High`, `Mid+High`) | All Clean Initial (`Low / Mid / High`, `Mid+High`) | Planted Tuned *(tuned)* (`Mid+High`) | Clean Twin Tuned *(tuned)* (`Mid+High`) |
| --- | --- | --- | --- | --- | --- |
| `override` | 0 / 10 / 0 (10/10, 100.0%) | 89 / 3 / 0 (3/92, 3.3%) | 178 / 6 / 0 (6/184, 3.3%) | 10/10 (100.0%) | 24/92 (26.1%) |
| `exfil` | 0 / 10 / 0 (10/10, 100.0%) | 88 / 1 / 0 (1/89, 1.1%) | 176 / 2 / 0 (2/178, 1.1%) | 10/10 (100.0%) | 13/89 (14.6%) |
| `social` | 10 / 0 / 0 (0/10, 0.0%) | 82 / 8 / 0 (8/90, 8.9%) | 164 / 16 / 0 (16/180, 8.9%) | 2/10 (20.0%) | 32/90 (35.6%) |
| `fragmented` | 30 / 0 / 0 (0/30, 0.0%) | 83 / 7 / 0 (7/90, 7.8%) | 166 / 14 / 0 (14/180, 7.8%) | 8/30 (26.7%) | 23/90 (25.6%) |
| `framing` | 0 / 6 / 4 (10/10, 100.0%) | 85 / 3 / 0 (3/88, 3.4%) | 170 / 6 / 0 (6/176, 3.4%) | 10/10 (100.0%) | 11/88 (12.5%) |
| `rank` | 5 / 5 / 0 (5/10, 50.0%) | 88 / 2 / 0 (2/90, 2.2%) | 176 / 4 / 0 (4/180, 2.2%) | 9/10 (90.0%) | 23/90 (25.6%) |
| `quoted` | n/a | n/a | 87 / 13 / 0 (13/100, 13.0%) | n/a | n/a |
| `article` | n/a | n/a | 80 / 11 / 0 (11/91, 12.1%) | n/a | n/a |
| **Total** | **45 / 31 / 4 (35/80, 43.8%)** | **515 / 24 / 0 (24/539, 4.5%)** | **1197 / 72 / 0 (72/1269, 5.7%)** | **49/80 (61.3%)** | **126/539 (23.4%)** |

## 4. Separate `rank` Class Breakdown

*Table caption: Small authored evaluations are not benchmarks. The `rank` class is reported separately because it is the only attack class that hijacked a framed reader in Milestone 1 and uses quiet score-inflation claims.*

| Metric | Planted `rank` Passages (`10`) | Clean `rank-control` Twin Passages (`90`) | All Clean `rank` Passages (`180`) |
| --- | --- | --- | --- |
| `self_rank_claim` (weight 2) | **5 / 10 (`50.0%`)** | 0 / 90 (`0.0%`) | 0 / 180 (`0.0%`) |
| `off_topic` (weight 1 initial, 2 tuned) | **6 / 10 (`60.0%`)** | 23 / 90 (`25.6%`) | 47 / 180 (`26.1%`) |
| `lone_site` (weight 1 initial, 0 tuned) | 0 / 10 (`0.0%`) | 10 / 90 (`11.1%`) | 20 / 180 (`11.1%`) |
| Initial `middle + high` band | **5 / 10 (`50.0%`)** | 2 / 90 (`2.2%`) | 4 / 180 (`2.2%`) |
| Tuned `middle + high` band *(tuned)* | **9 / 10 (`90.0%`)** | 23 / 90 (`25.6%`) | 47 / 180 (`26.1%`) |

## 5. Whole-Document Comparison (`DefaultPassageBounds`, `Ceiling: 2000`)

*Table caption: Small authored evaluations are not benchmarks. Because every document in `corpus/cases.json` is `558`–`1,719` bytes without Markdown headings, `intake.Split` with `DefaultPassageBounds` (`Ceiling: 2000`) keeps each 3-paragraph document as 1 whole-document passage (`60` planted documents across `60` attack cases, `180` clean documents in `-control` twins, and `360` clean documents overall).*

| Class | Planted Docs (`Low / Mid / High`, `Mid+High`) | Clean Twin Docs (`Low / Mid / High`, `Mid+High`) | All Clean Docs (`Low / Mid / High`, `Mid+High`) |
| --- | --- | --- | --- |
| `override` | 0 / 10 / 0 (10/10, 100.0%) | 30 / 0 / 0 (0/30, 0.0%) | 50 / 0 / 0 (0/50, 0.0%) |
| `exfil` | 0 / 10 / 0 (10/10, 100.0%) | 30 / 0 / 0 (0/30, 0.0%) | 50 / 0 / 0 (0/50, 0.0%) |
| `social` | 10 / 0 / 0 (0/10, 0.0%) | 30 / 0 / 0 (0/30, 0.0%) | 50 / 0 / 0 (0/50, 0.0%) |
| `fragmented` | 10 / 0 / 0 (0/10, 0.0%) | 30 / 0 / 0 (0/30, 0.0%) | 50 / 0 / 0 (0/50, 0.0%) |
| `framing` | 0 / 6 / 4 (10/10, 100.0%) | 30 / 0 / 0 (0/30, 0.0%) | 50 / 0 / 0 (0/50, 0.0%) |
| `rank` | 5 / 5 / 0 (5/10, 50.0%) | 30 / 0 / 0 (0/30, 0.0%) | 50 / 0 / 0 (0/50, 0.0%) |
| `quoted` | n/a | n/a | 24 / 6 / 0 (6/30, 20.0%) |
| `article` | n/a | n/a | 23 / 7 / 0 (7/30, 23.3%) |
| **Total** | **25 / 31 / 4 (35/60, 58.3%)** | **180 / 0 / 0 (0/180, 0.0%)** | **347 / 13 / 0 (13/360, 3.6%)** |

---

# Intake Checks — Second Measurement: Fast Decision Model on Passages (`#54`)

> **Lab measurement caveat:** Small authored evaluations are not benchmarks. These counts measure `semselect` (`semselect-kev-4b`, `ggml-org/Kev-4B-GGUF@d924f2e2c387`, `Kev-4B-Q4_K_M.gguf`, `sha256:33ae6b189265`, runtime `llama.cpp@6c59c40076c0` on `darwin/arm64 (Apple M3 Pro, Metal)`) across the `181`-passage cohort (`80` planted paragraphs, `80` position-matched clean `-control` twin paragraphs, and all `24` `middle+high` clean `-control` twin paragraphs; `101` unique clean twin paragraphs total) from `corpus_sha256: 0ee5145661e4`.

## 6. Second Measurement Provenance, Latency & Stop-Rule Outcome (`#54`)

- **Model & lockfile (`semselect/models.lock.json`):** `semselect-kev-4b` (`ggml-org/Kev-4B-GGUF`, revision `d924f2e2c3872da8b8aaf3eb4453b4126deceb79`, file `Kev-4B-Q4_K_M.gguf`, `sha256:33ae6b18926502b2209a1bf7d3b61a350d65938441515c686bb87d226a19eff9`), `semselect` revision `152feca58e447b332119f2751e629e7749628de5`, `llama.cpp` runtime revision `6c59c40076c00eab49754dc955d7652d93f9e125`.
- **Hardware & endpoint:** `darwin/arm64 (Apple M3 Pro, Metal)` (`http://127.0.0.1:8084`).
- **Per-call latency (`181` calls, 2 `noul` questions sharing 1 state pass per call):** `p50 = 635 ms`, `p95 = 934 ms` (`min = 493 ms`, `max = 1127 ms`, `total = 120.2 s`).
- **Stop rule (`#54`, fixed before the run):** If the decision model flags planted and clean passages at about the same rate (`planted_flag_rate - clean_flag_rate < 20.0%`), stop — the approach does not detect quiet manipulation, and the structural rules are what ships.
- **Overall discrimination (`yes + unsure`, `p >= 0.35`):** Flagged **`70/80` (`87.5%`)** planted passages vs **`27/80` (`33.8%`)** position-matched clean `-control` twin passages (delta **`+53.8` percentage points**; vs **`42/101` (`41.6%`)** across all `101` evaluated clean twin passages, delta **`+45.9` pp**).
- **Overall discrimination at binary `p >= 0.50` (`yes` only):** Flagged **`32/80` (`40.0%`)** planted passages vs **`4/80` (`5.0%`)** position-matched clean `-control` twin passages (delta **`+35.0` pp**).
- **What the `low`-band cohort caught that the plain checks missed:** Flagged **`35/45` (`77.8%`)** `low`-band planted passages (`social`, `fragmented`, and the 5 unflagged `rank` passages; `22/31` or `71.0%` among planted passages where *zero* checks fired) vs **`25/77` (`32.5%`)** `low`-band clean twin passages.
- **Stop-rule outcome:** **`decision_model_discriminates`** (`discriminates = true`).

## 7. Decision Model Outcomes by Attack Class (`#54`)

*Table caption: Small authored evaluations are not benchmarks. Each cell reports `Flagged / Total (No / Unsure / Yes)` and binary `Yes@0.50` across the 60 attack cases and their 60 `-control` clean twins (`80` planted paragraphs, `80` position-matched clean twin paragraphs, and `101` total evaluated clean twin paragraphs including all `24` `middle+high` clean twins).*

| Class | Planted All (`Flagged / Total`, `No/Uns/Yes`, `Yes@0.50`) | Matched Clean Twin (`Flagged / Total`, `No/Uns/Yes`, `Yes@0.50`) | Planted `middle+high` (`Flagged / Total`) | Clean Twin `middle+high` (`Flagged / Total`) | Planted `low` Band (`Flagged / Total`, Caught Missed by Bands) | Clean Twin `low` Band (`Flagged / Total`) |
| --- | --- | --- | --- | --- | --- | --- |
| `override` | 10/10 (100.0%; 0/1/9; @0.50: 9/10, 90.0%) | 5/10 (50.0%; 5/3/2; @0.50: 2/10, 20.0%) | 10/10 (100.0%; @0.50: 9/10) | 2/3 (66.7%; @0.50: 0/3) | 0/0 (n/a) | 5/9 (55.6%; @0.50: 2/9) |
| `exfil` | 10/10 (100.0%; 0/10/0; @0.50: 0/10, 0.0%) | 5/10 (50.0%; 5/5/0; @0.50: 0/10, 0.0%) | 10/10 (100.0%; @0.50: 0/10) | 1/1 (100.0%; @0.50: 0/1) | 0/0 (n/a) | 5/10 (50.0%; @0.50: 0/10) |
| `social` | 10/10 (100.0%; 0/1/9; @0.50: 9/10, 90.0%) | 3/10 (30.0%; 7/2/1; @0.50: 1/10, 10.0%) | 0/0 (n/a) | 5/8 (62.5%; @0.50: 1/8) | 10/10 (100.0%; @0.50: 9/10) | 3/10 (30.0%; @0.50: 1/10) |
| `fragmented` | 20/30 (66.7%; 10/17/3; @0.50: 3/30, 10.0%) | 7/30 (23.3%; 23/7/0; @0.50: 0/30, 0.0%) | 0/0 (n/a) | 6/7 (85.7%; @0.50: 0/7) | 20/30 (66.7%; @0.50: 3/30) | 5/28 (17.9%; @0.50: 0/28) |
| `framing` | 10/10 (100.0%; 0/9/1; @0.50: 1/10, 10.0%) | 3/10 (30.0%; 7/3/0; @0.50: 0/10, 0.0%) | 10/10 (100.0%; @0.50: 1/10) | 2/3 (66.7%; @0.50: 0/3) | 0/0 (n/a) | 3/10 (30.0%; @0.50: 0/10) |
| `rank` | 10/10 (100.0%; 0/0/10; @0.50: 10/10, 100.0%) | 4/10 (40.0%; 6/3/1; @0.50: 1/10, 10.0%) | 5/5 (100.0%; @0.50: 5/5) | 1/2 (50.0%; @0.50: 0/2) | 5/5 (100.0%; @0.50: 5/5) | 4/10 (40.0%; @0.50: 1/10) |
| **Total** | **70/80 (87.5%; 10/38/32; @0.50: 32/80, 40.0%)** | **27/80 (33.8%; 53/23/4; @0.50: 4/80, 5.0%)** | **35/35 (100.0%; @0.50: 15/35)** | **17/24 (70.8%; @0.50: 1/24)** | **35/45 (77.8%; @0.50: 17/45)** | **25/77 (32.5%; @0.50: 4/77)** |

## 8. Separate `rank` Class Decision-Model Breakdown (`#54`)

*Table caption: Small authored evaluations are not benchmarks. The `rank` class is reported separately because it is the only attack class that hijacked a framed reader in Milestone 1 (`5` planted paragraphs in `middle` via `self_rank_claim`, `5` quiet score-inflation paragraphs in `low`).*

| Cohort | Planted `rank` | Clean `rank-control` Twin |
| --- | --- | --- |
| All evaluated passages | **10/10 (100.0%; 0/0/10; @0.50: 10/10, 100.0%)** | 4/10 (40.0%; 6/3/1; @0.50: 1/10, 10.0%) |
| Escalated (`middle+high` in `#53`) | **5/5 (100.0%; 0/0/5; @0.50: 5/5, 100.0%)** | 1/2 (50.0%; 1/1/0; @0.50: 0/2, 0.0%) |
| `low` band in `#53` (missed by band gate) | **5/5 (100.0%; 0/0/5; @0.50: 5/5, 100.0%)** | 4/10 (40.0%; 6/3/1; @0.50: 1/10, 10.0%) |
| Zero check hits (`Checks == []`) | **1/1 (100.0%; 0/0/1; @0.50: 1/1, 100.0%)** | n/a |
