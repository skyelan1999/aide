# Windows stop recovery — Issue #755

Implementation repository: `skyelan1999/aide`; [issue #755](https://github.com/skyelan1999/aide/issues/755), [PR #756](https://github.com/skyelan1999/aide/pull/756).
Source under test: `9bec54afd277d98074e1fb2a690f13b4afcce4ec`, based on `6058f703af50c769fe393f5e487b814f718627cd`. No version bump or Release.

## Behavior

A failed `TerminateJobObject` call no longer immediately declares an unsafe process tree. Cleanup checks retained exact process handles and queried Job accounting; both must confirm exit. A successful termination request alone is also insufficient. Missing observation rights and live descendants remain unsafe. Job handles are non-inherited and retain kill-on-close behavior.

Atomic state replacement retries only sharing, lock, access-denied and user-mapped-file failures, with a two-second limit. Errors include numeric Win32 codes; permanent failure preserves the old file and removes the owned temporary file. It does not fall back to deleting the target or overwriting it non-atomically.

Full `stop` and explicit `recover-stop` archive only recognized assigned-Job blockers after process-lifetime, lock, helper, project-container and update-state checks. A recorded stopped flag or executable-path mismatch is not exit proof. Invalid/incomplete identity records refuse recovery; PID reuse does not authorize terminating the new owner. A never-started controller worker may have only `running:false`. Directory blockers additionally require the stopped matching controller generation.

Recovery writes an audit before atomically renaming the original marker, without replacing an earlier archive. Audit or archive failure retains the blocker. Unknown/unattached-child blockers and unsafe/pending update transactions remain for inspection. A later recoverable Job error cannot overwrite an existing unknown blocker, including concurrent initial writes. Existing data, volumes, image/slot markers and update journals are retained.

After updating and compiling the Windows source, run `stop.bat` then `start.bat` for a recognized old blocker. The explicit equivalent is `aide-launcher.exe recover-stop --root "C:\path to\aide"`, followed by start. Do not manually delete unsafe markers.

## Verification

- Local macOS ARM64: portable production transaction core, 35 checks; native archive/identity/ownership/proxy core, 66 checks; package extraction, 11 tests; desktop policy; BAT entrypoints, 2 tests — passed.
- Final local quick: 322 commands passed; fingerprint `7e3ea5b062fab5affe7ba1997b6a447a837e7d1c865d9129e23c795a327dd7a7`; receipt `.agent-state/verify-20261103T082025400174Z.log`.
- [Aide CI 41602550385](https://github.com/skyelan1999/aide/actions/runs/41602550385): quick, every-package Go compilation/critical regressions and gate passed.
- [Windows Desktop Check 41602550093](https://github.com/skyelan1999/aide/actions/runs/41602550093): AMD64 and ARM64 compilation, Windows x64 file/Job faults, production CLI recovery, and WebView2 desktop regression all passed. Nine API checks and eighteen CLI cases are recorded in the [controlled result summary](2026-11-03-windows-stop-recovery-755.json).

The first Windows attempt passed the file-sharing/Job fixture but its CLI test resolved the runner's system Docker before PATH. It refused the non-Linux engine before stopping any containers. The test now places its copied production launcher beside synthetic `docker.exe`, matching Windows executable-directory lookup. The second attempt successfully recovered the marker but its Python audit reader used the Windows default encoding for a UTF-8 Chinese path. The reader now specifies UTF-8; the final run passed all cases. These test corrections do not add a production test switch.

Physical user Windows Docker Desktop restart, ARM64 execution, real A/B migration and Windows Hello hardware are NOT_RUN for this change. The Windows CLI fixture uses synthetic Docker responses; it does not establish that every Windows installation or Docker environment starts successfully.

## Integration and cleanup

PR #756 was squash-merged to main as `92b5733707deed68584aa14aa53a91a2cced2319` after both final CI suites passed. The CI-built AMD64 launcher SHA256 is `15bba0a20da0f7b5f1e9cdb49993fc8c88f15d6865dfd863a889df93108b7784`. Owned local synthetic EXEs were removed after collecting evidence; pinned build dependencies/cache remain. No local application/service replacement, no Release/tag, no Docker-wide cleanup. The existing Mac runtime and unrelated Terminal task files are preserved.
