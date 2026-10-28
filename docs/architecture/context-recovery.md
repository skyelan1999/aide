# Context recovery and incomplete output

Issue: [#580](https://github.com/skyelan1999/aide/issues/580). Applies to normal execution and model-only parallel scheduling/workers.

The request builder meters all unsummarized session messages. It no longer takes an arbitrary 15k-token suffix, which could hide an early constraint or start with a tool result whose call was omitted. Existing preflight compares this complete request with the selected model window and output reserve before invoking session compaction. Histories beyond the 400-message batch limit continue through further progress-checked passes under one total deadline; a single pass is not mistaken for completion.

Compaction passes long message bodies and tool arguments through numbered, bounded source chunks. It does not remove a source message's middle before asking the summarizer to inspect it. This preserves coverage of source text, not lossless semantic memory: the model still produces a bounded, selective structured ledger. Arrays/field lengths remain bounded. Text-only goal arrays and scalar list items are normalized without a second model call; malformed JSON and numeric/object field shapes still fail without committing. Images are explicitly marked as not visually inspected by this text-only summary.

History cuts preserve assistant tool-call/result exchanges. A completed oversized final message may be folded completely; an unfinished tool exchange remains raw. Session writes retain the existing snapshot checks and atomic save; cancelled or invalid summaries do not overwrite source history.

During an active loop, tool results and parallel notes can outgrow the starting request. A local candidate is compacted before model calls, retaining the initial policy, current and latest user scope, and image-bearing messages. The same budget check applies after model routing and to planner/worker calls. The candidate is used only after summarization succeeds and its actual estimated size fits. If pinned current input/images/tool definitions alone cannot fit, execution reports the capacity error without silently discarding them. Usage calibration is bound to the session compaction generation. Pre-compaction usage and transient active/parallel summary anchors cannot inflate the next ordinary request. Task checkpoints persist the accepted execution context; source session history is separate. Active compaction emits started/completed/failed SSE metadata.

A text SSE response needs either a finish reason or the terminal `[DONE]` marker to count as complete. Premature EOF preserves partial text and usage, discards tool-call fragments, and pauses a visible partial answer at a resumable checkpoint. It does not blindly repeat visible text. User cancellation and existing length continuation remain separate paths. Recovered checkpoint content contains the partial answer exactly once.

## Verification

`internal/server/context_recovery_test.go` exercises the four pre-fix failures, an oversized final message, incomplete tool exchange, active-loop capacity, failure rollback and resumable partial output/accounting. Adjacent compaction, provider-stream, parallel, staged-output, retry and cancellation tests guard shared behavior.

`TestContextRecoveryLiveModels` is opt-in through `AIDE_CONTEXT_LIVE_DATA` and uses a read-only local credential envelope. It does not load real sessions, project memory or personality. Tests call `gpt-6.1-sol` and `deepseek-flash` with synthetic records, compact middle facts, then supply only the produced summary to a new 1x/3x API run. More than one completed analysis worker is required for the 3x cases. Safe synthetic results may be written via `AIDE_CONTEXT_LIVE_PROOF`; credential envelopes and raw local config are never published. Without the opt-in, the test is skipped, not a model acceptance pass.

This change does not deploy 9999, publish a release or alter provider settings. Rollback is reverting the fix commit; no persisted schema migration is introduced.
