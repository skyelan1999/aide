# SSH/plugin completion checks — 2026-11-12

Scope: #790 on main baseline `ccec15d2bc015f41b492ebe39eef6a9830a16206`. This is source and isolated install/function evidence, not an updated program Release or production deployment.

## Changed paths

- `api.sources` adds per-invocation `checkout` and `commit` for regular files. Each slot pins the enabled, AI-visible source, workspace/config/SSH generation, lock generation, original bytes and write permission. Four slots and 64 MiB total bound each call. Ordinary staged reads retain their existing interface. A failed/closed/changed source never falls back to the installation workspace.
- Commits require the original writable slot, recheck file bytes and configuration, write a temporary file and recheck immediately before publication. Pause/cancellation or a lock/unlock generation change refuses publication. Lock transitions cancel the mutation lease and wait before publishing the new lock state. The source/connection is checked again after writing; a change at that point reports an uncertain save that needs rereading. Independent external writers must remain stopped: SFTP does not provide a distributed compare-and-swap or SQLite lock.
- SQLite **1.0.3 source candidate** binds local relative paths to the selected local workspace. Authorized SSH workspaces and SFTP/workspace-SFTP references use a temporary offline database, then the host owns writeback. Nonempty WAL/journal files are refused. SQLite closes before commit; remote ATTACH is denied except the explicitly staged export output. Exports may choose a separate RW `outputSource`. Local direct-file SQLite locking and legacy absolute-root behavior are retained. Old hosts explicitly refuse the remote path.
- StarLink **0.1.6 source candidate** accepts only the matching remote workspace graph identity, with empty local `workspaceRoot`. Every graph request still uses `scope=ai`; lock/epoch/policy checks, full Unicode matching and cache invalidation remain in place. It receives no SSH credentials and reads no local substitute.
- Core DOCX structure, comments and resolve-comment tools accept an optional source ID. Remote parsing/transformation uses bounded temporary bytes; source/AI visibility/RW and workspace identity are checked around writeback. Workspace and independent reference paths were both exercised.
- SSH document/image/media attachments use a bound source snapshot. Existing non-streaming document drivers retain their prior read path with added identity/visibility checks. Source tools advertise `plugin-source-checkout-v1` when the per-call source capability is present.

## Evidence

[Two repeated race-enabled dual-OpenSSH runs](ssh-receipt.json) installed the exact candidate ZIPs into a real temporary Aide plugin host and ran actual Node handlers. The workspace on server A and independent reference on server B remained separate. Tests checked transactions, RO/hidden/WAL refusals, CSV bytes, document comments/resolve and attachment bytes, plus a real StarLink daemon querying an authenticated temporary Aide HTTPS graph. The first fixture reused a remote WAL filename across repetitions; it was corrected to use a unique reference directory per run. The final repeated run passed.

SQLite's original integration suite plus 30 focused security/source cases passed in Node 24; StarLink's 21 tests and deterministic package build passed. Core unit checks cover generation/source conflicts, cancelled calls, relocking, unsafe staged paths, temporary cleanup and guarded publication. The current published Terminal 1.8.0 package was SHA-verified and checked separately through the existing host test; physical serial/SSH hardware is not inferred from synthetic host checks.

A broader pre-existing `TestSSHLiveWorkspaceAndIndependentSource` run failed on both this candidate and a separate **clean main baseline** at `ccec15d2`. Failures include localized missing-file assertions, system-docs relocation and the synthetic approval-lifecycle expectation. Those failures were not turned into passing claims or attributed to these plugin changes. Raw control logs are retained locally; this report signs off the changed, passing source/plugin paths above rather than all historical SSH tests.

## Current locked public compatibility matrix

All **31 locked public ZIPs** were independently size/SHA-256/ZIP-structure checked against `scripts/ci/public-plugins.lock.json` at market revision `ba6b72e2611884288e00b014c8487251887bacda`; see [package audit](public-package-audit.json). `computer-control` is a retired marketplace entry, so this means 30 installable entries plus the migrated host capability. This is package/source classification, not 31 new physical SSH/device acceptance tests. The earlier [2026-11-04 matrix](../2026-11-04-ssh-workspace-compatibility-audit.md) remains historical.

| Locked plugin/version | Workspace/reference and independent-connection boundary |
| --- | --- |
| ali-bailian-supporter 0.1.2 | Offline price/catalog data; no workspace filesystem or inherited SSH credentials |
| almanac 1.1.0 | Host clock/calendar calculation; no remote filesystem |
| archive-tools 1.0.0 | Delegates file operations to the host's selected workspace/source driver and applicable permissions |
| auto-ai-model-router 0.1.2 | Model strategy/catalog capability; no independent workspace filesystem |
| browser-control 1.1.0 | Controls the explicitly allowed host browser; SSH file permission does not authorize a remote browser |
| current-time 1.1.0 | Host date/time calculation |
| deepseek-supporter 0.1.0 | Offline catalog; no model-account credential inheritance |
| environment-guide 1.0.0 | Host environment/source metadata, not a separate SSH shell |
| feedback 1.0.0 | Host workflow preset; execution uses host tools |
| goal 1.0.0 | Host workflow preset; execution uses host tools |
| lunar-calendar 1.1.0 | Offline bundled calendar data |
| markdown-history 1.0.0 | Host history executor, bound workspace/source identity and RW checks for restoration |
| office 1.1.0 | Host Office execution; this change closes DOCX structure/resolve and authorized source gaps; native Office apps/printing not implied |
| plan 1.0.0 | Host workflow preset |
| plugin-authoring 1.0.0 | Proposes host plugin/source configuration; a proposal does not inherit SSH file/shell authority |
| rviz 0.1.1 | Source-selected URDF/mesh snapshots; ROS/device SSH transport has separate configuration and credentials; hardware not tested here |
| skill 1.1.0 | Host registered-reference access and visibility; no implicit remote shell |
| sqlite 1.0.1 | Locked old package remains local; candidate 1.0.3 implements the checked SSH offline-file path above |
| starlink 0.1.4 | Locked old package rejects SSH; candidate 0.1.6 implements matching AI-scoped host graph access |
| step-viewer 0.1.1 | Input through host document binding; no independent arbitrary SSH credentials; real CAD runtime/device not tested |
| subagent 1.0.0 | Host workflow preset; scope stays with host task execution |
| terminal 1.8.0 | Workspace/reference identity stays in host context; separate device SSH/serial/network sessions require their own configuration; no inferred credentials |
| todo 1.0.0 | Host workflow preset |
| tts-tools 0.1.0 | Speech service; output uses host save/approval channels, not an inherited SSH session; paid voice services not called |
| video-preview 0.1.0 | Host-bound media input; no autonomous SSH connection; physical playback device acceptance not claimed |
| workflow 1.0.0 | Host workflow preset |
| file-preview 0.1.0 | Authorized source picker and bounded readonly stage; no SSH credentials passed |
| codex-bridge 0.4.1 | Host-local pairing/account/provider bridge, not a general remote file driver; SSH selection does not turn a host path into a remote directory |
| openai-supporter 0.1.0 | Offline catalog; no account login or paid request performed |
| computer-control 1.2.0 | Retired package: migrated to authorized local-host control; does not control an SSH desktop |
| mapcanvas 0.2.1 | Host source browsing/staging and bound document saves; source identity and write permission remain with host |

No proprietary plugin implementation was inspected or included. Public marketplace pins and installed packages remain unchanged until their own release/update step; installing a candidate into a test host is not publication. The new host source and candidate plugin source must be used together for the new paths. Full Aide Delivery, physical Windows/macOS device access and production-host/model acceptance were not run.
