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
- No Office settings were entered. Actual headed Chromium opened DOCX/XLSX/PPTX,
  switched to edit and saved user input. Independent file-library readback found
  `Offline Word baseline Offline local save`, `Offline Calc saved` and
  `Offline Slides baseline Offline saved`. Browser external requests were blocked;
  none were attempted during these document flows.
- The exact candidate ZIP uploaded successfully and registered the six existing
  Office tools. Native document transformations/readback and stale-hash rejection
  passed for all three formats; PPTX preview remained open during the write.
  Actual AI model inference was not invoked. After retiring the unused HTTP
  wrappers, internal tool dispatch and final public-route rejection are checked
  separately; earlier HTTP transformation checks do not claim a public API.
- Crash/recovery: killing only the owned editor revoked the old proxy capability.
  Aide restarted it automatically in 18.38 seconds, allowed a new session and
  continued to reject the old capability.
- Desktop and 900 px viewer controls were inspected. At 390 px the Aide toolbar
  wraps without horizontal page overflow; the engine's desktop slide navigator
  may need collapsing. Full mobile Office usability is not claimed. Upstream
  welcome/tour overlays were dismissed through their normal controls.

Final source/full verification and exact final-image acceptance are in progress.
Receipts/screenshots are under `.agent-state/office-offline/`. This review will be
updated only with observed final results. Native Windows/macOS execution,
enterprise capacity/SLA, remote SSH and paid model/service calls are not claimed.
The daily runtime has not been rebuilt, restarted or replaced.
