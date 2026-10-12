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

## Remaining

- Run the same UI probe against an authorized SFTP source.
- Test failure and cleanup-failure reporting without leaving temporary files behind.
- Broader connection/plugin capability acceptance remains open; this change does not close the continuity goal or release gate.
