# Crash detection and diagnostic codes

Task: [crash-diagnostics-20261016](../../tasks/crash-diagnostics-20261016.json). User requests crash detection and code display. [Product guide](../../architecture/crash-diagnostics.md) describes codes, local metadata, manual copy and detection boundaries.

The monitor loads independently before settings on both workbench and starmap. It captures uncaught runtime errors, unhandled asynchronous errors and first-party script/style loading failures; aborts/images stay outside crash classification. A single notice opens a diagnostic dialog with code/incident/source/stack; no automatic reload, task replay or cancellation. Locked pages hide it. Metadata history is bounded to12 and does not retain raw messages/stacks. Heartbeat restoration is conservative about same-tab reload/history and ignores new-tab inherited storage.

Foreground health probes have5-second timeout and three-failure threshold, use no credentials and pause on lock/background. Service instance changes are observations with an unconfirmed cause. Request panic replies have a fixed code and matching log incident; pre-commit entity headers are cleared. Committed streams abort intact and hijacked owned connections close; panic values and request payloads are not returned or logged.

## Evidence so far

Focused JS lifecycle/privacy/duplication and unknown-source/cache-query stack-position regressions pass. Prior quick88 passed before final refinements. Isolated Go race targeted tests passed2.364s including actual HTTP500 and committed-stream/healthy-follow-up behavior; a final prepared-download-header case is being checked separately.

An owned isolated browser page actually triggered runtime/rejection/script-resource errors,503 probe failures and fixture instance changes. It retained the test draft and hid diagnostics on lock. Native Safari click-copy then Command-V into an owned input proved code, source/stack and masked synthetic credentials; the IAB virtual clipboard was empty and was not used as copy proof. Refresh returned metadata history. A390px dark dialog and notice fit within the viewport after correcting only the fixture textarea sizing. See [browser proof](crash-diagnostics-browser-proof.json).

No deliberate production fault or model invocation. Temporary test tabs closed and original Safari workbench restored. Candidate build/runtime normal acceptance, fresh full, main gate/push and offline delivery remain pending. The earlier aurora is independently delivered to main8ccca842 and local5183a60; its frozen gate is not reused for crash changes.

## Frozen source checks

Final focused Go race passed3.127s, including real HTTP roundtrip, prepared download-header cleanup, intact aborted streams, subsequent healthy requests and per-request concurrency/connection ownership. Final quick88 passed. No production crash injection. A later source build/HTTP asset identity and normal native workbench acceptance are still required; earlier screenshot observations do not replace those gates.
