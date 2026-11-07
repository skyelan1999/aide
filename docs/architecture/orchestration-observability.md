# Core orchestration: timing and result receipts

The two parallel paths remain separate: `conversation_parallel.go` runs model-only analysis; `execution_environments.go` runs tool-capable scratch workspaces. Both now expose a common, content-free result receipt in the debug dataflow export. A completed worker means its request returned. It does not mean its claims were independently checked, its file proposal was applied, or the user received the requested result.

Each task also stores at most 256 lightweight timing events. Events cover model requests, context fitting when the request exceeds the context budget, planning and merging preparation, task steps, tool calls, approval or user wait, queue wait, workspace snapshot copy, worker execution, and collection. `orchestrationDropped` counts evicted oldest events. Unlike full provider request bodies, these events continue after the 12-snapshot cap. Events include kind, stage, UTC start/end, duration and outcome, without prompts, command arguments, file paths, or model output. They are saved at the next normal task checkpoint; the hot path does not trigger extra full-session writes.

The scratch-environment scheduler receives the latest eight samples per cost kind as `observedCosts`. It can group subsequent tiny work or continue in main when copy, queue, or collection time approaches worker time. These measurements overlap and must not be summed into an end-to-end duration.

Model-only scheduling skips the planner for a short single request without tool evidence or substantial deliverable terms. Explicit independent items and new tool evidence still get scheduling consideration. This removes planner overhead for the narrow class of trivial work; it is not a claim that a selected X setting always beats 1X.

## Measuring the next optimization

Use equivalent 1X and multi-X workloads and compare end-to-end wall time and accepted output, including approvals and integration. Examine the ledger's `snapshot_copy`, `queue_wait`, `model_request`, `context_fit`, `collect`, and `step` events before changing the workspace copy strategy. The current `copyExecutionTree` hashes bytes while copying through `io.MultiWriter`; it does not reread the copied file to hash it. A shared or lazy snapshot would change source freshness and isolation semantics, so it needs its own version/conflict test and repeated matched workload evidence before adoption.

The receipt deliberately sets `verified=false` for both worker types. Only main-task review and actual tests can establish verification. `proposed=true` means a normal pending proposal exists, not that a file was applied. The debug export uses a hash of each assignment/title to match a receipt to its retained task record without repeating its text.
