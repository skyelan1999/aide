# Runtime Port Integration Audit — 2026-10-03

## Scope

This pass followed the host-port value through the browser origin, WebAuthn, saved settings, Docker Compose mapping, offline launchers, and release packaging. It focused on defects with the same shape as the reported Passkey failure: a value being correct in one layer but stale or missing at an adjacent host/container boundary.

This was a targeted integration audit, not a claim that every feature in the repository is bug-free.

## Confirmed findings

| # | Finding | Root cause | Resolution |
| --- | --- | --- | --- |
| 1 | Passkey registration/assertion failed when the browser used a dynamically mapped host port such as `9999`. | WebAuthn relied on a backend origin list derived from the container's default port. The container does not receive the host's `AIDE_PORT`, so that value can differ from the URL the browser actually uses. | Validate the localhost origin on the start request, bind it to that challenge, and require the matching origin on finish. The RP ID remains `localhost`. |
| 2 | The accessibility host-port control could show the default/saved port after the offline launcher had selected a fallback port. | The control displayed persisted configuration rather than the active browser listener. | Display the port from the current page origin; keep the saved setting as the persistence source used by the launcher. |
| 3 | The Windows offline launcher did not select a fallback port when its configured port was occupied, and it did not apply later port changes made in Settings. | `start.ps1` lacked the fallback selection already present in the Unix offline launcher and did not launch a Windows host-side port watcher; the combined full runtime package also omitted that watcher. | Select and persist a free port during first launch, include `watch-port.ps1` in the Windows and combined full release packages, and run it for offline bundles. The watcher applies the saved port through Compose without removing shared volumes. |
| 4 | Windows offline child processes could fail when the package was installed under a directory containing spaces. | `Start-Process -ArgumentList` received an unquoted `-File` script path for the update agent and port watcher. | Quote both script paths when starting the PowerShell child processes. |

## Boundary checks

- The app listens on container port `8080`; Compose maps the host `AIDE_PORT` to that fixed container port.
- Health checks probe the container listener, while browser URLs and update agents use the actual host mapping.
- Existing update agents query `docker compose port` and clear inherited host-port overrides before resolving the current mapping.
- Workspace and `/data` remain Compose-managed mounts when only the host port mapping is reconciled.
- Unix offline startup already selected a fallback port and ran its host-side watcher; this audit brought Windows offline startup in line with that lifecycle.
- Source/developer startup does not gain a separate auto-upgrade path from this change; the Windows port watcher is packaged and started only for offline release bundles.

## Verification evidence

- `docker run --rm -v "$PWD:/src" -w /src golang:1.26-bookworm go test -mod=vendor ./internal/server -run 'Test(WebAuthnOriginAllowsDynamicLocalPort|RegisterStartChallengeKeyMatch|AssertionStartChallengeKeyMatch)$' -count=1` — PASS.
- `node --check internal/server/web/app.js` — PASS.
- `node scripts/test_accessibility_port.cjs` — PASS; covers an explicit non-default port and default HTTP/HTTPS ports.
- `python3 scripts/test_windows_port_lifecycle.py` — PASS; static checks confirm fallback persistence, watcher launch, Compose port reconciliation, and Windows package inclusion.
- `bash -n scripts/package-release-assets.sh` — PASS.
- `python3 scripts/agent-route.py verify quick` — PASS (16 checks, including documentation checks and the two new regression checks).
- `git diff --check` — PASS.
- Runtime observation during investigation: local Compose mapped `127.0.0.1:9999` to container `8080`; the container had no `AIDE_PORT` environment value while the host `.env` held `AIDE_PORT=9999`.

## Verification limits and handoff

- The Windows PowerShell launcher and watcher received static regression checks only; this host has no `pwsh` or `powershell` executable, so the scripts were not parsed or run by PowerShell here.
- No Safari browser acceptance run was performed against a newly built image. The UI change is source-only in this worktree; an existing RC14 runtime cannot verify it.
- No new release image/package was built, published, or pushed. Those steps need a subsequent release run after review.
