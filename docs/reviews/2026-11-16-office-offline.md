# Built-in offline Office acceptance

Requirement: Office preview/edit/save directly in Aide, entirely offline, with
no separate service deployment or address/settings entry. The user additionally
requires Aide-only use, without an independently exposed plugin interface.
Tracking: https://github.com/skyelan1999/aide/issues/937 (follow-up to #935).

## Implementation

The unmodified upstream CODE 26.04.5.2 AppImage is pinned for Linux ARM64/AMD64
in `docker/office-engine/source.json` and bundled at build time. Runtime never
downloads it. Aide supplies a managed launcher, random loopback listener and a
separate loopback-only WOPI callback. The child uses a minimal environment,
without provider credentials or proxy variables. No Docker socket or additional
host port is required. Aide closes the child, callback and owned temporary files
on shutdown, and restarts a failed child when a document is opened again.

The browser uses `/office-engine/` on the existing Aide origin, protected by an
HttpOnly, SameSite document-session capability. Aide credentials and cookies are
stripped upstream. Public WOPI is refused; general document read/edit HTTP routes
are removed. Structured AI access remains inside the Aide native tool dispatcher,
with the existing task, workspace, sandbox and source checks. Administrative and
general conversion endpoints are denied. Installed legacy service settings are
ignored; Office 1.3.0 has no service settings schema.

The shared runtime budget reserves memory for the Office child: the normal and
offline Compose paths use 4 GiB with a 2600 MiB Go heap limit. This is a runtime
budget, not enterprise capacity or concurrency acceptance. The app still uses
its original viewer layout and plugin enablement rules.

## Observed checks

- Both real upstream architecture archives and executable hashes were checked.
  Offline staging succeeded from the local pinned cache for ARM64 and AMD64.
  AMD64 packaging is not AMD64 execution acceptance.
- Five installer regressions passed: offline/no-network staging, missing cache,
  corrupt archive, symlink member and wrong executable digest.
- Directed Go checks covered the WOPI read-only/activity/version rules, native AI
  dispatch and sandbox, capability cookie, encoded WebSocket paths, public WOPI
  refusal, disabled/expired sessions and legacy settings isolation.
- A real isolated ARM64 runtime, as an unprivileged user with all Docker
  capabilities dropped, ran on an internal Docker network with no gateway.
  An external TCP connection returned ENETUNREACH (101). A test-only TCP relay
  provided browser access; it was not an Office service or product dependency.
- Final source `4ae9b7d8` was built statically (`CGO_ENABLED=0`). Its accepted
  ARM64 image is `sha256:2c66af4c6e4bc223ff228dc241c65c09e5bb0bda555a94ea2d49baf041ef50f9`.
  A fresh nonroot runtime reported integrity=ok and ENETUNREACH for an external
  connection. Before installing any Office plugin or entering Office settings,
  headed Chromium opened DOCX/XLSX/PPTX, switched to edit and saved input.
  Independent library readback found the saved Word suffix `Accepted source save`
  with retained Chinese text, cell A1 `Accepted source Calc saved`, and slide title
  `Accepted offline Slides baseline Source saved`. No external browser requests
  were attempted. First engine startup took 22.758 seconds in this test.
- The exact final ZIP passed fresh installation and public 1.2.0 upgrade. Wrong
  digest was refused; disabled state, settings, installedAt, sentinel user data
  and all three document hashes survived upgrade. Historical service settings
  were ignored, no service schema properties were required, and all six tools
  registered as executable. The generic plugin manager still has its normal
  settings editor; no Office service setup is needed.
- The final-source race test binary ran inside the actual packaged runtime,
  exercising native dispatch/sandbox and six Aide-only boundary/lifecycle cases.
  Live public WOPI/admin/conversion and document read/edit requests returned 404;
  editor resources without a document-session capability returned 401. Earlier
  source-parent transformations while preview remained open are separate evidence;
  their retired HTTP wrappers are not a supported public API. The actual
  document-to-AI button attached the DOCX to Aide's task input. Paid model
  inference was not invoked.
- Crash/recovery: killing only the owned editor revoked the old proxy capability.
  Aide restarted it automatically in 18.38 seconds, allowed a new session and
  continued to reject the old capability.
- Final-source real shutdown exited 0 in 1.168 seconds and removed owned engine
  temporary directories. The copied native acceptance fixture was separately
  retained until the disposable container was removed.
- Desktop and 900 px viewer controls were inspected. At 390 px the Aide toolbar
  wraps without horizontal page overflow; the engine's desktop slide navigator
  may need collapsing. Full mobile Office usability is not claimed. Upstream
  welcome/tour overlays were dismissed through their normal controls.

## Full verification and publication

`AIDE_TEST_IMAGE=aide-office-offline:candidate-20261116
AIDE_TEST_TOOLCHAIN_IMAGE=aide:cn-sdk-verify-20261115 AIDE_TEST_WORKERS=1
python3 scripts/agent-route.py verify full` passed on exact source
`4ae9b7d8135b80974ee7f813320dd7a1bdec3d7e`, fingerprint
`7ba74f492e96b6e13a54a7cbfc6cfe3998f9db444600c1b0174b38bd231885e3`.
All 350 commands succeeded: 24 server race shards selected all 1,798 registered
items (1,704 passed, 94 environment/opt-in skips, none missing), other Go race
packages, 11 platform contracts, vet and runtime/optional STEP checks. Skipped
live tests are not claimed as passed. Raw evidence remains local under
`.agent-state/office-offline/`; the durable summary is
[verification JSON](2026-11-16-office-offline-verification.json).

[Host PR #938](https://github.com/skyelan1999/aide/pull/938) merged as
`16ce28313792022e98ceec5eed0b190a7ac61e94`, preserving intervening terminal PR #939.
Office/build files were compared with the accepted source and unchanged;
[merged-main CI](https://github.com/skyelan1999/aide/actions/runs/43853371710)
passed quick/go/gate separately. The full receipt belongs to source `4ae9b7d8`,
not an unrun full profile on the merge commit.

[Office 1.3.0](https://github.com/skyelan1999/aide-pkg-office/releases/tag/v1.3.0)
is public from plugin merge `5dd1a71fb81eba82e5258ddffece8805be750bb1`.
The ZIP is 12,831 bytes, SHA-256
`a1f8e9e505af3c80855f7d070cdce7c093fd70547f97af58a56166b157a82a89`.
[Market PR #55](https://github.com/skyelan1999/aide-pkg-shop/pull/55) merged as
`9e01d59ad4019456fc079420dce1054adbc86b04`; its Pages deployment passed.
Anonymous Release, pinned-raw, Pages and actual public-browser downloads matched
the exact ZIP. Public search/details showed 1.3.0 and `office-offline-v1`.
An initial check before Pages deployment completed saw the previous catalog;
the subsequent completed-deployment checks passed.

Complete Aide host builds must include `office-offline-v1`; installing this ZIP
alone does not update an older host's engine. No Aide application version/tag,
Release, public default runtime-image update or daily deployment was performed.
The application Delivery/release-check gate is not claimed. Native Windows/macOS,
AMD64 engine execution, enterprise capacity/SLA, remote SSH, full mobile usability
and paid model calls were not accepted. The owned test browser and services are
closed; the SDK is stopped with its cache retained. Original local work and the
healthy daily runtime are preserved.
