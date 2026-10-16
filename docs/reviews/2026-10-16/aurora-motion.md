# Dynamic aurora background

Task: [aurora-motion-20261016](../../tasks/aurora-motion-20261016.json). The user requests visibly dynamic aurora. The former single faint gradient drifted only slightly over28 seconds. The replacement uses two independent transform-only light layers with12/19-second alternate periods and offset initial phases. Soft contained gradients avoid rotated rectangular edges; existing intensity controls remain. No per-frame JavaScript, network or model calls.

Production JS/CSS was opened in an isolated browser component. Independent computed-transform samples confirm both layers move. Actual motion-control input pauses both, and a later sample confirms stable transforms. Off removes both layers and hides the background; re-enabling creates exactly two running layers. Light/dark screenshots were visually inspected;390px has no horizontal document overflow. See [component evidence](aurora-motion-component-proof.json). This is not native workbench/runtime acceptance.

Existing local video/GIF/WebP lifecycle tests pass, including visibility/lock/reduced-motion, late local media responses, pagehide/pageshow and resource release. Lifecycle regression additionally checks preference refresh does not accumulate aurora layers. Temporary component tab/server closed and viewport override reset. Full gate, candidate/runtime/native acceptance and main/offline delivery remain pending.

## Candidate

Image sha256:5183a60ed4be142258bf52ef41ea380a59c9cbc2d5e4ce040c644519752495f7, build414da4dabe1c9ffd9c315692efe1dd2e1816e46d, source SHA 42932fbc14e9692584880674b975bec2ca2fd814b0eaa4f89056d3fb3fb867f1. Complete offline bundle 706069936 bytes, SHA-256 ce4007628097d74852a60430825948c28476606fd5e46c28b9d21f39157ab9bc. Checksums, image tags, launcher/license and local actual import passed; receiving device untested. See [bundle proof](aurora-motion-bundle-proof.json).

## Native local candidate acceptance

Designated start.command updated local9999 to5183a60ed4be for candidate acceptance after focused/quick checks. Fresh preflight14 sessions, running/nonterminal/queued0; original protected configuration/auth/session bytes, mounts and workspace config remain exact. Health/source label and12 HTTP assets match source, including CSS3/backgroundJS3.

Reused native Safari without another workbench tab. Original background was off at22 percent. Temporarily selected aurora, then captured two screenshots without intervening native input. A fixed background region outside the opaque settings sheet changed11155 of60800 pixels; per-channel mean absolute difference about0.21 under the settings backdrop. This supports visible-frame change, not frame-rate performance. Component computed-transform samples provide the separate direct animation proof. Restored off/22 percent, closed settings and kept original root/session and blank prompt. No model calls or user-file changes. See [native/runtime evidence](aurora-motion-runtime-proof.json).

Fresh full is running in exec session4595; do not reuse the previous save-status full for these changed product inputs. Main gate/push remains pending.

## Frozen full gate

First full had all88 commands exit0, but its final input-change guard failed during shared-checkout task work; that receipt was retained as failed and was never promoted. Crash development moved to a separate ignored Git worktree. A fresh frozen-input full88 passed: receipt20261016T021515838177Z, fingerprinted0b67ae5e4295c9e9725f7f629cbc0ea26e8bc4b9a9f676c35d2773d7c65045, server race601.801s and all-package vet. See [full proof](aurora-motion-full-proof.json). Main gate/push is the remaining delivery step.

## Main delivery

Independent clean-main gate passed. Push exited0 and remote main exactly matched08ccb097. This final documentation-only receipt keeps the verified product fingerprint; no new version/tag/Release. Local runtime5183a60 is healthy and the full offline package was locally imported. Receiving-device import remains untested; prior8d1957c9 image/package is retained.
