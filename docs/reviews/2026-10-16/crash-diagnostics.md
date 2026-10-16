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

## Offline candidate

Image sha256:3f4545df534b26654e12aec95f3fc56bb272839363416afda3e6af8168c8ee95, build2bbc5d63b4c06ff36e4b80dd6a312d25e18ab2bb, source SHA 659fbc83a5fb18e67ed1b0797dad69d2273d3ecf25ab10817b2a9515e1e32f65. Complete offline bundle 706082863 bytes, SHA-256 5d0f8bfb64a532402a17d05f7071852652bef8d1e4924e39b32b5b3d526195ec. Checksums, image tags, license/launcher consistency and actual local import passed; receiving device untested. See [bundle evidence](crash-diagnostics-bundle-proof.json).

## Local normal-runtime acceptance

Fresh idle preflight and designated start.command updated local9999 to3f4545df534b. Health and instance identity pass;14 HTTP assets including monitor JS/CSS exactly match source. Settings/source/auth/all session bytes, mounts and workspace configuration remain exact.

Native Safari reused the original workbench after verifying an empty prompt. No unexpected error notice appeared. Settings / System logs now opens Crash and error diagnostics; the healthy page correctly shows no incident and disables copy. Closed diagnostics, returned Settings to Appearance and closed Settings; background off22 and original scene/tabs remain. No deliberate production fault or model call. See [normal runtime proof](crash-diagnostics-runtime-proof.json); isolated actual errors/native clipboard evidence is separate.

Fresh full90 is running in exec session54311 on fixed primary-checkout inputs. No source edits or branch switches during this full. Main gate/push is pending; previous aurora full is not reused.
