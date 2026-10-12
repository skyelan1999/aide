# Connection capability write probe · 2026-10-12

## Change

The capability dashboard now keeps read checks and write checks as separate, five-minute observations. A write probe is offered only for configured writable workspace and local or SFTP sources. The UI explains the temporary operation and asks for confirmation before it runs.

Local probes create a cryptographically random name with exclusive creation under the configured root, write a short marker, close the file, and remove it. SFTP probes use a unique temporary directory, upload the marker, then remove the file and directory. A probe reports success only when writing and cleanup both succeed. The probe does not claim that normal file operations or a plugin have passed.

## Evidence

- After the final cleanup-path changes, `go test -race -count=1 -timeout=90s ./internal/server -run '^TestCapability(WriteProbe|LocalChecks|PluginAndSourceBoundaries)'` passed with Go 1.26 in Docker (4.724 s).
- `gofmt` applied to the changed Go file; `node --check` passed for `capabilities.js` and `locales/en.js`; `git diff --check` passed. `python3 scripts/agent-route.py verify quick` passed all 38 configured checks. The rebuilt candidate also passed Docker `go vet ./...` and `go build` via `./start.command`.
- In system Safari at `https://localhost:9999`, after rebuilding the final cleanup-path changes and unlocking Aide, Settings → Accessibility → Interface Tests displayed independent read and write states for the workspace. At 2026-10-12 15:55:40 Asia/Shanghai, the confirmed temporary test for `/Users/skyelan/debug` showed “已在目标根目录创建并删除独占临时探针；写入与清理均通过” (1 ms).
- At 2026-10-12 15:55:54 Asia/Shanghai, Safari also confirmed the configured automatic-system-docs source `/Users/skyelan/debug/doc`. Its independent result showed “已在目标根目录创建并删除独占临时探针；写入与清理均通过” (2 ms).
- The two Safari results prove only the configured local workspace and automatic-system-docs directory probes at those times. They do not prove remote SFTP, general file CRUD, plugin execution, or long-term persistence.
- Both local write probes were rerun against the rebuilt candidate. Safari showed independent successful write and cleanup receipts for each configured root.
- The opt-in live acceptance script `AIDE_SSH_REPEAT=2 ./scripts/test-ssh-workspace.sh` passed twice against disposable OpenSSH/SFTP servers on an internal-only Docker network (46.690 s total; race detector enabled). The test now calls the real `/api/capabilities/check` write path for both an independent SFTP reference source on server B and automatic system documents on workspace SFTP server A, then compares each remote root listing before and after to confirm no probe residue. Receipt: `.agent-state/ssh-storage-audit/live/aide-ssh-test-20261012162829-28432.log`; SHA-256: `4c2d061e0451a5530966aef2ebe0d55f7293c869412caad875a6c487efc999eb`.
- This is backend live-fixture evidence for the remote probe branches, not a Safari UI probe against a user-configured SFTP source. The current Safari workspace and automatic-system-docs paths are local; no user SFTP configuration was changed for this test.

## Remaining

- Run the same UI probe in Safari against an authorized, configured SFTP source when one is available.
- Test failure and cleanup-failure reporting without leaving temporary files behind.
- Broader connection/plugin capability acceptance remains open; this change does not close the continuity goal or release gate.

## Full repository verification

- `python3 scripts/agent-route.py verify full` passed on commit `112ab6b80aa00489a61995e7591c1ca15a608b45`: 38/38 configured checks, including `go test -race -count=1 -timeout=15m ./...` and `go vet ./...`.
- The server race suite completed in 745.907 seconds. Receipt: `.agent-state/verify-20261012T080019248412Z.log`; fingerprint: `f178ad438be52cf8a8cbbc61feabd386f48591a928a58ae2e4bc4c352fad7cfa`.
- An earlier run without Docker socket access failed before starting the container test. The successful full run used the same verifier with Docker access enabled; the earlier environment failure is not counted as a test failure.
