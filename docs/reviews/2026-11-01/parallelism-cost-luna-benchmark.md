# GPT-6 Luna 1x / 10x task benchmark

Date: 2026-11-01

Model: `gpt-6-luna`

Input: synthetic only; within each task type, 1x and 10x received the same prompt and synthetic history.

Usage source: OpenAI upstream-reported token usage, including preflight context compaction. No monetary cost is estimated.

Evidence availability: references below identified as local-only are ignored `.agent-state/` files from the original test machine. They are not included in a clean checkout or public release, and cannot serve as independently downloadable proof. In-repository reports remain linked; the recorded measurements and limitations below are unchanged.

| Task | Setting | Wall time | Total tokens | Prompt / completion | Workers | Coverage |
|---|---:|---:|---:|---:|---:|---|
| Simple extraction | 1x | 7.83s | 6,670 | 6,384 / 286 | 0 | pass |
| Simple extraction | 10x | 7.58s | 8,915 | 8,653 / 262 | 0 | pass |
| Ten-domain acceptance draft | 1x | 14.97s | 8,378 | 6,802 / 1,576 | 0 | pass |
| Ten-domain acceptance draft | 10x | 21.63s | 11,401 | 9,263 / 2,138 | 0 | pass |

## Difference

- **Simple:** 10x took 0.25s less (within normal timing noise) and used 2,245 more tokens (+33.7%). Neither setting started workers.
- **Complex:** 10x took 6.66s longer (1.44x) and used 3,023 more tokens (+36.1%). Both answers passed content coverage, but the 10x run started zero workers.
- The test therefore did **not** achieve actual 10-way concurrency with Luna. It measured the same model configured at 1x versus a 10x ceiling; the timing/token changes came without worker fanout. Task-only tokens excluding preflight were 1,983/4,306 for simple and 3,749/6,721 for complex (1x/10x).

## Limits

The initial matrix has one sample per setting; provider/network variability can change wall time and model output. Its Go test command returned nonzero because its acceptance assertion expects workers for a multi-x complex run, while Luna initially started zero. All four answer coverage checks passed.

## Complex 10x diagnostic rerun

A targeted rerun of the same complex 10x synthetic case completed with **10 workers started and completed**, full label coverage, and context budgets fitting:

| Model / setting | Wall time | Total tokens | Prompt / completion | Workers |
|---|---:|---:|---:|---:|
| GPT-6 Luna / complex 10x | 35.76s | 42,586 | 35,925 / 6,661 | 10 / 10 |

The targeted Go test passed. Compared with the earlier 1x sample (14.97s, 8,378 tokens), this small workload took longer and used about 5.1x the tokens. This confirms fanout can occur, while also showing the planner, ten separate model calls, and merge overhead dominate a task that is too small to benefit from parallelism. The first 10x run's zero-worker result also shows fanout is not deterministic yet.

## Direct-dispatch logic check

The implementation now recognizes explicitly labeled independent items in the user's latest prompt and bypasses the separate planner call. The same Luna complex 10x case was rerun after that change: **10/10 workers completed**, all ten labels appeared exactly once, request budgets fit, elapsed time was **31.63s**, and upstream-reported usage was **37,970 tokens** (32,412 prompt / 5,558 completion). The live test passed. This is one sample and does not establish a repeatable speedup; the shorter elapsed time and lower usage versus the planner-based rerun (35.76s / 42,586 tokens) may also reflect output variability.

## User acceptance target

The target is now **a very complex task that takes about 10 minutes at 1x should finish within 1 minute at 10x**, with equivalent required coverage and evidence quality. This benchmark does not validate that target: its 1x workload took about 15 seconds, and its 10x diagnostic rerun took about 36 seconds. A dedicated 10-minute-class paired workload and repeated runs are still needed before claiming the target is met.

Raw synthetic proofs: [initial matrix](parallelism-cost-luna-benchmark.json), planner-based rerun (local-only: `.agent-state/parallelism-cost-luna-diagnose-20261101.json`), and direct-dispatch rerun (local-only: `.agent-state/parallelism-cost-luna-direct-fanout-20261101.json`).

## Shared worker-context fitting and fixed-path rerun

The scheduler now removes explicitly labeled task lines from the shared worker history, fits one shared template against the smallest selected worker context window, then substitutes each worker's assignment. The main executor retains the original user request and history for final synthesis. During review, a loop later in preparation was found to reconstruct worker messages from the untrimmed input after fitting; that overwrote the fitted context. It has been removed.

The regression test `TestConversationParallelFitsSharedWorkerContextOnce` supplies an oversized synthetic history against a 4,096-token fixture window. It verifies compaction happens fewer times than the worker count, all ten workers receive the compacted evidence, the oversized raw history is absent, and workers receive no tools. Focused Go tests passed after the fix.

The fixed implementation was rerun against GPT-6 Luna using the live synthetic complex 10x harness:

| Model / setting | Wall time | Total tokens | Prompt / completion | Workers | Coverage / budgets | Snapshots |
|---|---:|---:|---:|---:|---|---:|
| GPT-6 Luna / complex 10x | 30.28s | 37,268 | 31,599 / 5,669 | 10 / 10 | all labels once; fit | 11 |

Task-only usage excluding preflight was 32,582 tokens (27,164 prompt / 5,418 completion); all usage figures are upstream-reported. This particular live run's worker requests already fit a 65,536-token window, so it validates the fixed end-to-end dispatch path, not live oversized-history compaction. The local oversized-context regression separately covers that branch. The rerun is slightly slower than the prior 28.53s shared-context run and does not establish a timing improvement; run-to-run variation remains material.

The 1x reference is still the earlier 14.97s small synthetic case, not a matched 10-minute-class workload. The requested 10-minute 1x to 60-second 10x target remains unproven. Main executor tools, verification, writes, approvals, and final synthesis are serial and cannot be accelerated by the current read-only worker pool.

Fixed-path proof: Luna shared-context fixed rerun (local-only: `.agent-state/parallelism-cost-luna-shared-context-fixed-20261101.json`).

## Fast assembly and quality-checked repeat

The serial final synthesis request was the dominant latency in the earlier 10x runs. For direct-dispatched, explicitly labeled, independent requests that forbid tools and ask for a draft/analysis, the implementation now assembles worker sections in label order only after verifying exact label coverage, requested section elements, and the normal task-delivery check. If a requested element is missing, it falls back to the normal main-model synthesis. Other task types keep the normal synthesis path.

The live test now validates the synthetic complex brief's requested checks, evidence, unknowns, two distinct checks per section, opening identifier/threshold, label coverage, fitted budgets, and explicit non-execution caveat. A fixture test also confirms missing requested content is rejected.

| Luna synthetic complex case | Worker/executor phase | Harness preflight | Reported tokens | Workers | Quality |
|---|---:|---:|---:|---:|---|
| Quality-checked 1x reference | 12.17s | 6.37s | 8,463 | 0 | all ten sections include two checks, evidence, unknowns; facts retained |
| Fast assembly 10x | 4.48s | 7.37s | 29,026 | 10/10 | labels once; budgets fit; all requested content present |
| Worker-quality 10x repeat | 4.26s | 6.49s | 28,766 | 10/10 | worker sections passed content gate |
| Final-text quality 10x | 4.88s | 5.43s | 28,785 | 10/10 | final answer passed section-level content and non-execution checks |

The latest final-text checked 10x run was about 2.5x faster in model execution and 1.8x faster in harness wall time than the quality-checked 1x call, and avoids the 15.85s or 21.31s synthesis calls observed in earlier 10x runs. The 10x case used roughly 3.4x the total upstream-reported tokens and returned a longer draft. Harness wall time includes the separately measured context-recovery preflight (about 11s for 10x and 19s for 1x). There is one quality-checked 1x sample and one final-text checked 10x sample, plus an earlier worker-level repeat. This is not a 10-minute baseline and does not establish the requested 60-second goal. Parallel calls, rate limits, context fitting, output volume, and serial tool/verification/write work remain relevant bottlenecks.

Proofs: quality-checked 1x reference (local-only: `.agent-state/parallelism-cost-luna-fast-assembly-quality-pair-1x-20261101.json`), fast assembly 10x (local-only: `.agent-state/parallelism-cost-luna-fast-assembly-10x-20261101.json`), worker-quality 10x repeat (local-only: `.agent-state/parallelism-cost-luna-fast-assembly-10x-quality-repeat-20261101.json`), and latest final-text quality 10x (local-only: `.agent-state/parallelism-cost-luna-fast-assembly-quality-final-10x-20261101.json`). An older exploratory pair (before the fast-assembly shortcut was active) remains here (local-only: `.agent-state/parallelism-cost-luna-fast-assembly-pair-20261101.json`).

## Regression test note

The targeted parallel scheduling, shared-context fitting, fast-assembly, quality fallback, cancellation, and end-to-end fixture tests pass. A full `internal/server` package run was attempted but remained in an SSH/SFTP fixture test (`TestSFTPDirectoryReconnectIsReadOnly...`) for over seven minutes. That test container was stopped to avoid a long-running fixture connection; the package-wide result is incomplete and is not reported as passing.

## Numbered-list dispatch and stricter Luna workload

The scheduler now also recognizes an explicitly independent, consecutive numbered list starting at 1, without requiring CASE labels. Sequential wording, gaps, duplicate/malformed lists, or an unmarked list remain on the planner/main path. A strict synthetic Luna pair required ten sections, two checks per section, evidence, risks, dependencies, pass criteria, unknowns, an opening identifier/threshold, and an explicit statement that verification was not executed. Section validation counts headings rather than raw numeric substrings, so the `7.` heading is not confused with the `27.4` threshold; two distinct `检查建议` lines count as two checks.

| Luna numbered extreme draft | Execution | Preflight | Reported tokens | Workers | Quality / coverage |
|---|---:|---:|---:|---:|---|
| 1x | 16.25s | 5.02s | 8,933 | 0 | pass |
| 10x | 18.34s | 4.25s | 37,315 | 10/10 | pass |

This sample is more demanding than the prior short prompt, but still completes in seconds at 1x; the 10x run is about 2.1 seconds slower and uses about 4.2x the tokens. It demonstrates that parallel fanout does not automatically accelerate a single concise response, and it does not validate the 10-minute-to-1-minute target. Proof: numbered extreme Luna pair (local-only: `.agent-state/parallelism-cost-luna-numbered-extreme-20261101.json`).

## Bounded local read batching

One main-executor bottleneck now has a narrow parallel path: a model batch of 2–10 independent local `read_file`, `list_files`, and safe relative `.docx` `docx_structure` calls may execute concurrently when there are no reference-source arguments or matching before/after task hooks. Dispatch checkpoints and intent events are emitted in model order first; tool records and results are then appended in the original model order. Remote SSH, reference-source reads, unsafe DOCX paths, comment operations, mixed/unknown tools, shell, plugins, hooks, and batches above ten remain serial. DOCX parsing also retains its own two-slot parser limit. Focused eligibility, ordering, end-to-end read batch, and race-detector tests passed. Builds, tests, writes, approvals and final verification remain serial. This can reduce repository/document inspection latency but cannot be treated as broad 10x acceleration.

The latest extension admits `docx_structure` only for safe relative `.docx` paths in a local batch; invalid paths, non-DOCX files, `docx_list_comments`, SSH and matching hooks remain on the serial path. Eligibility and race tests passed. This is a code-path expansion, not a measured Luna wall-time improvement; no end-to-end speedup is claimed for this change.

Local `semantic_search` calls without a reference source may now join the same bounded read batch. All queries in that batch share one immutable, batch-scoped corpus scan and run independent TF-IDF ranking; no cache survives the tool batch. Reference-source queries, empty/malformed queries, mixed/stateful calls, and matching hooks remain serial. The integration, cache-once, ordering, eligibility and race tests cover this path. No live timing measurement was collected for semantic-search batches.

## Preserve the configured worker output budget

Review found that parallel workers were always capped at 2,048 output tokens even though Aide's default and precise profiles allow 8,192. This could truncate a genuinely detailed independent subtask while the main executor retained its larger budget. Worker output now inherits the requested profile budget up to 8,192; when the selected model has a known small context window, the reserve is limited to half that window so evidence and instructions still fit. A focused test covers default, profile, cap, and small-window cases.

The Luna `extreme` pair was rerun after this change. Both answers passed the existing content, identifier, coverage, and context-budget checks; the 10x run started and completed all ten workers and each worker snapshot reserved 8,192 output tokens. The workload itself remained short: 1x used 20.50s execution / 28s test wall time and 8,922 tokens; 10x used 28.60s execution / 35s test wall time and 37,395 tokens (5,056 completion tokens). This is slower at 10x and substantially more expensive in tokens. It verifies the larger reserve reaches Luna, but does not demonstrate that a task requiring that output completes within 60 seconds or that 10x achieves the 10-minute-to-1-minute target. Proof: worker-budget Luna pair (local-only: `.agent-state/parallelism-cost-luna-worker-budget-20261101.json`).

The latest focused race-detector set, including the worker budget, worker fanout, and local read batch tests, passed. Full-package regression remains unverified.

## Chinese numbered-list dispatch

The direct-dispatch recognizer now accepts Chinese numeral markers (for example `一、二、三、` and `（一）（二）`) when the prompt explicitly describes the items as independent. It requires a continuous sequence from one, consistent punctuation/wrapping and numeral style, and retains each original marker for assembly checks. Gapped or mixed-style lists stay on the normal planning path. Focused dispatch, fast-assembly and quality-gate race tests passed. This removes the serial planner request for another common form of user-authored decomposition; no new Luna wall-time measurement was collected, so it is not evidence of meeting the 60-second target.


## Decimal facts no longer force serial synthesis

The latest unchanged `extreme` Luna/off workload exposed a production assembly mismatch: workers completed in 3.2–4.3 seconds, but substring matching treated `7.` inside the required `27.4 MHz` fact as an extra section. That rejected direct assembly and added a 15.9-second main synthesis call. Numbered labels now match section headings with boundaries; duplicate headings and genuine cross-assignment headings still reject direct assembly. CASE labels keep their existing exact-token checks. This fixes the new direct-assembly feature rather than relaxing its content gate.

| Luna/off, identical extreme prompt | Execution | Test wall including compaction | Reported tokens | Workers | Result |
|---|---:|---:|---:|---:|---|
| Before fix 1x | 18.509s | 29.62s | 9,160 | 0 | content/coverage pass |
| Before fix 10x | 20.300s | 26.78s | 37,942 | 10/10 | extra main synthesis |
| After fix 1x | 17.202s | 23.34s | 9,123 | 0 | content/coverage pass |
| After fix 10x | 4.364s | 9.85s | 29,784 | 10/10 | content/coverage pass; direct assembly |

The paired execution ratio is about 3.94x, and test-wall ratio about 2.37x. These remain short synthetic drafting tasks, not a ten-minute baseline. Marker/content checks do not establish general engineering correctness. Proofs: before fix (local-only: `.agent-state/parallelism-cost-luna-current-20261101.json`), after fix pair (local-only: `.agent-state/parallelism-cost-luna-heading-fixed-20261101.json`).

The user explicitly prioritizes elapsed time over token cost. Live benchmarks now accept `AIDE_CONTEXT_LIVE_REASONING_EFFORT` (default `off`) and `AIDE_CONTEXT_LIVE_TIMEOUT_SECONDS` (30–1800, default 900), making reasoning and long-run limits explicit without changing product settings. A high-effort attempt failed during compaction preflight with EOF; neither task started, so it contributes no performance sample.

A second 10x run also passed the content, coverage and context-budget checks, with 10/10 completed workers and direct assembly: execution **3.965s**, preflight **4.640s**, total **9.06s**, 30,106 reported tokens. Targeted race tests for decimal facts, direct assembly and quality rejection passed. Repeat proof (local-only: `.agent-state/parallelism-cost-luna-heading-fixed-repeat-20261101.json`).


## High-effort format and critical-path follow-up

High reasoning is now explicitly selected by environment, and is tested separately from the earlier off-effort pairs. The first high-effort pair failed the harness's label check because the completed answers used bold numbered headings. Production and benchmark heading parsing now share the same function, including markdown heading/bold wrappers, while retaining numeric boundaries and rejecting duplicated/misattributed sections. A first-worker instruction that rendered `1.` next to sentence-ending punctuation could also elicit `1..`; labels are now quoted with an explicit trailing-space requirement.

The subsequent high-effort pair produced complete heading coverage, but was not an accepted quality-equivalent pair: 1x used the shortened field `通过` rather than `通过标准`, and 10x required main synthesis when individual workers condensed checks or field names. 1x execution was 16.439s, 10x 27.588s. The 10x worker request times were 5.2–11.1s followed by a 16.353s main synthesis call. These timing records are now populated for every parallel request snapshot as well as each worker's whole duration; previously parallel request snapshots reported zero duration. Proof: high-effort diagnostic (local-only: `.agent-state/parallelism-cost-luna-high-markdown-fixed-20261101.json`).

Recommendation bullet recognition now accepts two explicit `•` items within the check-recommendation section; bullets/semicolons in later evidence fields cannot satisfy that requirement. Positive and negative fixtures and targeted race tests cover it. Worker instructions preserve user-requested field names and separate check recommendations. The live extreme prompt now explicitly requires the same field layout for both 1x and 10x, so later pairs must be treated as a new workload version. The initial attempt of that version failed during compaction preflight with EOF; it contributes no task-performance sample. None of these short synthetic prompts establishes the ten-minute baseline or general correctness target.

The bounded retry of the new structured prompt **passed** at Luna/high: 1x execution **19.013s**, preflight **5.388s**, test wall **24.78s**, reported tokens 9,602; 10x execution **9.906s**, preflight **5.645s**, test wall **15.89s**, reported tokens 35,390. All ten workers completed; direct assembly succeeded without a main synthesis call. Both passed current content, label coverage, history preservation and context-budget checks. Execution speedup is about **1.92x**, test-wall speedup about **1.56x**. This is one accepted pair for this new prompt/high-effort configuration, not a ten-minute task or a repeated high-effort performance claim. Accepted structured high-effort pair (local-only: `.agent-state/parallelism-cost-luna-high-structured-retry-20261101.json`). Targeted race tests passed after the final instruction changes.


## Exact engineering-resource workload (new workload class)

`AIDE_CONTEXT_LIVE_TASKS=engineering` now selects ten deterministic independent 0/1 resource-choice cases. Each has 18 candidates, integer cost/capacity/value, two resource caps, three prerequisite pairs and three conflict pairs. The model receives only the problem data, never the solutions. The independent local oracle enumerates all 262,144 subsets per case; acceptance requires valid unique IDs, feasible selection, exact recomputed totals and globally optimal value. Multiple equally optimal selections are accepted. This is an exact reasoning stress test, not source-code development or an already calibrated ten-minute baseline. Unit tests check a hand-solved optimum plus invalid totals, duplicates, infeasibility, prerequisite/conflict violations and duplicate JSON results. Workload fingerprints are now recorded by the harness, and task execution time is frozen before oracle validation to avoid charging local scoring to model time (the first run below predates that timing refinement).

First Luna/high pair on the engineering workload:

| Mode | Execution | Test wall | Workers returning answers | Exact final solutions |
|---|---:|---:|---:|---:|
| 1x | 11.677s | 16.56s | n/a | 0/10; explicitly unsolved |
| 10x | 85.132s | 90.10s | 3/10 | 0/10; final synthesis declared all unsolved |

Both runs **failed** acceptance. Labels/context budgets passing did not count as success. The three completed worker candidates (cases 3, 8, 9) independently match optimum values **294, 323, 287**, with recomputed resource totals (74,55), (70,51), (72,50). A separate Python subset enumeration agreed with the Go oracle for those cases. Six other workers consumed exactly the 8,192 completion-token ceiling without a final answer; another failed request returned no usage. Worker durations were 39.2–70.7s, followed by a 14.228s main synthesis. The main synthesis discarded even the three correct candidate solutions; the final answer therefore remains a failure. This shows output/reasoning budget exhaustion and incomplete-result handling as material limits of the current model-only worker path. Increasing the cap alone is not a demonstrated speed fix. No ten-minute-to-one-minute speedup can be inferred because the 1x baseline did not solve the task.

Engineering failure proof (local-only: `.agent-state/parallelism-cost-luna-engineering-20261101.json`). Synthetic fixtures and oracle: `internal/server/parallel_engineering_benchmark_test.go`; no real project data or artificial delays are used.


## Retry checkpoint preserves completed assignments

Code inspection following the engineering failure found that the previous-title exclusion ledger included failed/cancelled attempts, a single remaining explicit assignment could not be dispatched, and the next wave's merge included only the new wave. The runtime now excludes only completed assignments, admits a single outstanding item when the original user-authored explicit decomposition had at least two, and rebuilds the merge in the complete original assignment order from each assignment's latest attempt. Historical attempts remain intact for accounting. Unchanged evidence still does not trigger retries; the existing three-wave scope budget remains. The first failed wave still goes to main reconciliation; this change does not certify the engineering candidate values or fix main-model rejection of candidates by itself.

New parts record the human-scope fingerprint. Reused legacy or prior-scope candidates are supplied to main reconciliation as unverified notes and cannot take the direct-assembly shortcut. Only a retry of the first original assignment can repeat shared opening facts. The regression uses an actual HTTP model fixture: first wave has one success/one failure, unchanged checkpoint makes no calls, new evidence retries only the failed assignment, and the complete result contains both assignments. A legacy-scope follow-up requires main review and preserves both candidates. Targeted race scheduling/fanout/cancellation/end-to-end checks passed. No new Luna performance claim is made for this recovery-path change.

The existing `execsandbox` read-only path is not a complete no-network compute-worker isolation boundary: the inspected Landlock ruleset covers filesystem and process scopes, and does not configure network access restrictions. It is therefore not automatically exposed to model-only workers as a calculation tool in this change. Solving the exact engineering workload still requires a general computation path or a suitable model strategy, with actual answer verification.


## Medium-effort engineering probe and compaction transport recovery

The first medium-effort pair aborted in compaction preflight with EOF and provides no task-performance sample. Compaction now replays a tool-free summary request once after EOF/unexpected EOF, following a cancelable 200ms backoff; it does not replay API-status failures, timeouts, canceled requests, executor tool calls or visible streaming responses. The existing strict-format regeneration remains separate, so the absolute bound is four HTTP attempts if each of its two generation requests first encounters EOF. HTTP connection fixtures and race tests covered transient recovery, permanent EOF bounded at two attempts, no replay on HTTP 401/cancellation, and unchanged source history on malformed-summary failure.

The subsequent same-input Luna/medium engineering pair completed but **failed** full acceptance:

| Mode | Execution (frozen before local scoring) | Test wall | Worker responses | Exact final solutions |
|---|---:|---:|---:|---:|
| 1x | 6.100s | 12.15s | n/a | 0/10, explicitly unsolved |
| 10x | 87.274s | 94.99s | 10/10 | 5/10 |

Correct final cases were 1, 5, 7, 8 and 10; cases 2, 3, 4, 6 and 9 explicitly reported unsolved and were rejected by the oracle. All worker calls returning text is therefore not equivalent to all assignments being solved. The paired prompt SHA-256 was identical: `9ac801d2fa182f70d9fc86453a700a72b66ce1529ef4db2aaa2aa7bad6884765`. Direct assembly succeeded, so the 87-second critical path was a worker, not main synthesis. This single medium-effort sample improves exact returned case count versus the earlier high-effort sample, but neither effort mode meets the target and neither has a successful 1x ten-minute baseline. No product reasoning preference was automatically changed.

Medium-effort engineering pair (local-only: `.agent-state/parallelism-cost-luna-engineering-medium-retry-20261101.json`). The exact combinatorial stress test reveals model-only computation limitations; it is not a substitute for the requested real coding/development workflow benchmark.


## Coding pipeline pilot: actual proposals, approval and executed tests

`AIDE_CONTEXT_LIVE_TASKS=coding` creates a fresh temporary Go workspace with ten independent pure-function defects: clamping, overflow-safe pagination, stable deduplication, expiry boundary, CAS, interval containment, Unicode reversal, nearest-rank percentile, half-open overlap and saturating backoff. The model can use only `list_files`, `read_file` and `write_file`; other built-in tools are disabled for this isolated test. Workers keep their current model-only analysis role. Proposals pass the normal file-version/application API, and the harness approves only the ten named workspace files. This is an automated fixture approval, not a measurement of human review time.

After application, the harness validates the fixture contract (expected package/function, no global declarations/init/extra functions, only the pure `sort` import) and copies sources into another temporary module containing hidden tests authored by the harness. It then executes `go test`; the model cannot edit those tests or module metadata through workspace tools. Baseline test execution fails all ten cases; reference repairs pass all ten. Fixture/race tests cover these controls. This is a coding-pipeline smoke workload, not the requested very-complex ten-minute workload or general source-execution security acceptance.

The initial paired test's 1x compaction failed with EOF, while 10x succeeded. A separately rerun 1x then succeeded with exactly the same prompt SHA-256 `4a086a8acffb5ee41688d1c60892fb305f0c0a8d0d61a9e044c466069c68c0b6`:

| Luna/high | Model/task phase | Through apply + executed tests | Compaction preflight | Test wall | Hidden regressions |
|---|---:|---:|---:|---:|---|
| 1x retry | 43.981s | 44.190s | 6.600s | 51.07s | 10/10 pass |
| 10x | 37.223s | 37.412s | 9.282s | 46.94s | 10/10 pass |

The apply/test-inclusive ratio is approximately 1.18x for this single pair, with no ten-minute baseline. The 10x run completed ten analysis workers; workers did not execute tools or apply patches. Ordinary main-executor tool calls, file proposals, approval and tests remained downstream. The snapshot log reaches its 12-body cap in 10x, so it is incomplete for attributing later main-executor latency; it must not be used as a complete critical-path trace. 1x token accounting was partly estimated; tokens are not the optimization gate per the user's instruction. Model elapsed time is frozen before application/test scoring; `endToEndElapsedMillis` records through those stages and excludes preflight, while test wall includes preflight and harness overhead.

10x successful pilot (local-only: `.agent-state/parallelism-cost-luna-coding-20261101.json`), 1x successful retry (local-only: `.agent-state/parallelism-cost-luna-coding-1x-retry-20261101.json`). Only these isolated temporary fixtures were applied; no user's project or running Aide service was modified.


## Parallel file-draft candidate

The new local edit candidate recognizes user-authored independent file assignments, waits for full original reads, then asks model-only workers for strict path/content JSON drafts. The main executor validates all targets/scopes and registers proposals through ordinary write_file dispatch with normal policy, version, checkpoint, hooks, audit and tool-budget handling. Workers neither call tools nor apply files. A final main response still runs; normal approval/application follows it. This removes code regeneration by the main model for qualifying edits. Current eligibility requires that the complete original is retained in its read snapshot (hash of Before equals BaseHash), pins that original in the worker assignment, and falls back for clipped previews/other unsupported conditions. It does not restrict those projects' ordinary editing workflow, but the fast path is not yet a complete solution for large-source contexts.

The first live attempt failed both compaction preflights with EOF. The retry on the exact same coding prompt passed all ten hidden regressions in both modes:

| Luna/high coding | Task phase | Through approval/application/tests | Test wall |
|---|---:|---:|---:|
| 1x | 27.794s | 27.978s | 35.92s |
| 10x draft candidate | 22.228s | 22.447s | 28.20s |
| 10x repeat, original source pinned | 19.458s | 19.658s | 25.62s |

The paired apply/test-inclusive ratio is about 1.25x. The repeat has no newly paired 1x reference and must not imply a stable larger ratio. All ten worker parts identify their assigned draft paths; all ten applied source regressions passed. A lightweight registered-proposal counter was subsequently added and locally tested, so these earlier live files do not yet contain that counter. The 12-body request-snapshot limit still truncates later main-call timings. This is still the small coding pilot, not the ten-minute-class target. Generic drafting, cancellation, fanout, end-to-end and ROS2 fixtures were checked with race; a stale ROS2 assertion was corrected to inspect the reference-context message containing the user task, rather than require it to be copied into the isolated scheduler instruction. New policy/version/JSON/budget/cancellation fixtures passed.

Paired coding drafts (local-only: `.agent-state/parallelism-cost-luna-coding-parallel-drafts-retry-20261101.json`), pinned-original 10x repeat (local-only: `.agent-state/parallelism-cost-luna-coding-parallel-drafts-pinned-20261101.json`), [candidate design](../../designs/parallel-file-drafts-20261101.md). No production service or user's project was updated during these model tests.
