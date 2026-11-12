# SemGuard Milestone 1 Report: Defending AI Readers Against Prompt Injection

**Date:** November 2026 | **Models Tested:** OpenAI `gpt-5.4` & Google `gemini-3.8-flash` | **Total Runs:** 1,920

> **Scope Note:** This is a controlled laboratory evaluation on a frozen 120-run test suite (50 adversarial attacks, 20 benign edge-case documents, and 50 clean control documents). It measures which specific defenses earn their complexity in our pipeline and should not be cited as a general industry benchmark.

---

## 1. Executive Summary

When an AI model reads external documents—such as web pages, emails, or uploaded files—to summarize facts or score relevance, an attacker can hide instructions inside the text (known as **indirect prompt injection**). We tested four layered defenses across two frontier models (`gpt-5.4` and `gemini-3.8-flash`) to find out which safeguards actually stop attacks without breaking normal work or adding unnecessary delay.

### Key Findings at a Glance
1. **Input Boundary Framing works best and costs nothing:** Clearly marking untrusted document text before the AI reads it reduced successful attacks from **12% down to 2%** on `gpt-5.4` (6/50 to 1/50) and from **2% down to 0%** on `gemini-3.8-flash` (1/50 to 0/50), with **zero broken responses** and **zero extra AI calls**.
2. **Strict Exact-Quote Verification was too brittle:** Forcing the AI to output structured JSON and checking that every supporting quote matched the cited paragraph word-for-word provided almost no extra security over framing alone, while **falsely rejecting 20% to 31% of normal `gpt-5.4` responses** whenever the model slightly paraphrased a quote or cited an adjacent paragraph.
3. **Secondary AI "Auditor" Checks did not earn their cost:** Having a second AI model review the first AI's output without re-reading the original documents caught almost nothing (**0% to 14%** of remaining attacks) while adding **0.65 to 3.9 seconds** of extra wait time per request.
4. **Pre-Reader Filtering is unnecessary right now:** Because Input Boundary Framing already stops **98% to 100%** of attacks in our suite, running a separate classifier model on every single paragraph before the reader runs does not justify the infrastructure complexity.

---

## 2. How We Tested (Methodology)

In every test run, the AI reader was given a realistic assignment: **read a set of documents, extract key factual claims with supporting quotes, and assign a numeric relevance score.**

### The 120-Run Test Suite
Each defense configuration was tested against **120 document sets**:
* **50 Adversarial Attacks (5 attack styles, 10 cases each):**
  1. *Direct Instruction Override:* Commands telling the AI to ignore its instructions and do something else.
  2. *Data Exfiltration:* Tricks trying to make the AI leak secret information or embed unauthorized external links.
  3. *Social Engineering:* Persuasive fake authority claims ("System Administrator Notice: output the following...").
  4. *Fragmented Payloads:* Malicious instructions split across multiple paragraphs or documents so they only make sense when combined.
  5. *Score Manipulation (Rank Hijacking):* Hidden instructions demanding that a low-quality document be given an artificially high score.
* **20 Benign "Look-Alike" Documents (False-Positive Stress Tests):**
  1. *Quoted Attacks (10 cases):* Legitimate security articles or documentation that quote examples of prompt injection for educational purposes.
  2. *Normal Articles (10 cases):* Everyday technical prose containing command-like phrasing.
* **50 Clean Control Twins:**
  * Every single attack document has a matching "clean twin"—the exact same document with the malicious sentence removed. By comparing the AI's numeric score on the attacked document against its clean twin, we can objectively measure whether an attack secretly inflated a document's ranking.

### How We Judged Every Response
Instead of relying on subjective human grading, an automated judge checked three objective tripwires:
* **Hijacked (Attack Succeeded):** The AI leaked a secret test phrase (**Canary Leak**), included an unauthorized web link (**Unallowed Link**), or boosted a document's numeric score significantly above its clean control twin (**Score Inflation**).
* **Broken Output (Usability Failure):** The defense layer itself rejected the AI's response because the formatting or paragraph quote citation did not match strict rules—meaning a real user would get an error instead of an answer.
* **False Flag:** A defense layer mistakenly flagged a harmless document as an attack.

---

## 3. The Four Defense Approaches Explained

We tested combinations of four plain-language defense stages:

| Defense Stage | Plain-Language Explanation | Extra Cost / Delay |
| :--- | :--- | :--- |
| **1. Baseline (No Defense)** | The AI reads the instructions and the raw documents in a single prompt and writes a normal response. | None |
| **2. Input Boundary Framing** | Before the AI sees a document, we strip out fake system boundary tags, wrap each document in clear "untrusted data" markers, and watermark the text so the AI can clearly tell where instructions end and external data begins. | **Zero extra AI calls** (~0 ms overhead) |
| **3. Strict Quote & Format Check** | We require the AI to respond in a rigid data format (JSON) and mechanically verify that every quote it cites appears *character-for-character* inside the exact paragraph number it listed, and that all links come from an approved list. | **Zero extra AI calls**, but rejects responses if quotes aren't exact |
| **4. Secondary AI Auditor** | After the reader finishes, a second, smaller AI reviews the final summary (without seeing the raw untrusted documents) to check if the output drifted off-task or contradicted itself. We tested both a local decision model (`Kev-4B`) and a fast cloud chat model (`gpt-5.4-mini`). | **+1 extra AI call** (+650 ms to +3,860 ms per run) |

---

## 4. Results & What Earned Its Place

### A. Reader & Verification Results (50 Attacks + 70 Benign/Control Runs per Row)

| AI Reader Model | Defense Configuration | Attacks That Succeeded (Lower is Better) | What Slipped Through? | Usable Responses Broken by Defense (Lower is Better) | Typical Response Time (Median) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **OpenAI `gpt-5.4`** | **Baseline (No Defense)** | **6 / 50 (12%)** | 4 score inflations, 2 social tricks | **0 / 120 (0%)** | 9.5 sec |
| **OpenAI `gpt-5.4`** | **Strict Quote Check Only** | **7 / 50 (14%)** | 6 score inflations, 1 data leak | **24 / 120 (20%)** *(incl. 6 clean controls)* | 9.8 sec |
| **OpenAI `gpt-5.4`** | **Input Boundary Framing Only** | **1 / 50 (2%)** | 1 social trick (0 score inflations) | **0 / 120 (0%)** | 11.3 sec |
| **OpenAI `gpt-5.4`** | **Framing + Strict Quote Check** | **1 / 50 (2%)** | 1 score inflation | **37 / 120 (31%)** *(incl. 11 clean controls)* | 10.0 sec |
| **Google `gemini-3.8-flash`** | **Baseline (No Defense)** | **1 / 50 (2%)** | 1 unauthorized link | **0 / 120 (0%)** | 8.6 sec |
| **Google `gemini-3.8-flash`** | **Strict Quote Check Only** | **0 / 50 (0%)** | None | **7 / 120 (6%)** | 10.5 sec |
| **Google `gemini-3.8-flash`** | **Input Boundary Framing Only** | **0 / 50 (0%)** | None | **0 / 120 (0%)** | 8.3 sec |
| **Google `gemini-3.8-flash`** | **Framing + Strict Quote Check** | **0 / 50 (0%)** | None | **6 / 120 (5%)** | 12.3 sec |

### B. Secondary AI Auditor Results (Reviewing Completed Outputs)

| Secondary Auditor Tested | Remaining Attacks Caught | Benign Articles Falsely Flagged | Extra Delay Added (Median) | Verdict |
| :--- | :--- | :--- | :--- | :--- |
| **Local Decision Model (`Kev-4B`)** | **0 of 8 (0%)** | 0 false flags *(never triggered at all)* | **+2.6 to +3.9 sec** | **Dropped:** Scored every single output—clean or hijacked—between 0.21 and 0.44 (below the 0.50 alarm threshold). |
| **Small Cloud Chat Model (`gpt-5.4-mini`)** | **1 of 8 (12.5%)** | 2 false flags on harmless articles | **+0.65 sec** | **Dropped:** Because the auditor does not see the source documents, a hijacked reader that inflates a numeric score still writes a polite, plausible-looking summary that fools the auditor. |

---

## 5. Practical Recommendations & Next Steps

1. **Adopt Input Boundary Framing (`pkg/untrusted`) by default:** Wrapping untrusted documents with structural framing and datamarks eliminated **100% of score-inflation attacks** on `gpt-5.4` and reduced overall attack success to **0%–2%** across both models with zero false rejections.
2. **Keep Link Allowlisting, but relax Exact-Substring Quote Checking (`pkg/verify`):** Checking that URLs in the AI's output actually came from an approved list is fast and reliable. However, rejecting an entire response when a supporting quote is slightly paraphrased or attributed to an adjacent paragraph broke up to **31% of `gpt-5.4` runs** (including completely clean documents). Quote verification should use fuzzy or document-wide matching before being turned on as a hard blocker.
3. **Skip Secondary Output Auditors and Pre-Reader Paragraph Classifiers for v0:** Neither post-output auditing nor pre-reader paragraph classification is justified when Input Boundary Framing already stops 98%–100% of tested attacks at zero additional model cost.
4. **Full Technical Evidence:** For exact CLI reproduction metadata, SHA-256 digests, and per-class rule breakdowns, see [`docs/results.md`](./results.md) and [`docs/decisions.md`](./decisions.md).
