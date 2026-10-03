# Harness P0 policy slice · 2026-10-03

## Outcome

Implemented a narrow backend consistency fix on `test/harness-p0-capability-policy-20261003`: the existing per-tool deny list now filters plugin tools from both Aide and assistant schemas, and the assistant's execution entry point checks the same deny decision as Aide before dispatching any tool. A model cannot bypass the setting by submitting a tool call that was omitted from its advertised schema.

This closes a concrete authorization bypass; it does not complete the broader capability-policy design in the 2026-10-02 architecture review. The product still has no user-configurable, shared `allow/ask/deny` policy registry. In particular, `write_file` proposals are a specific approval path, while shell and trusted Node plugins have different execution semantics. Do not describe this slice as a universal approval system or as execution isolation.

## Changed behavior

- A disabled plugin tool is absent from `contextTools()` and `assistantAgentTools()` schemas.
- Aide `executeToolCall()` and assistant `execAssistantTool()` both reject a disabled tool, even when invoked directly with a forged/unadvertised call.
- Existing enabled tools retain their prior behavior. Existing `disabledTools` settings remain backward compatible.

## Verification

Regression coverage was added to `internal/server/current_time_plugin_test.go` for schema filtering and forged-call rejection in both execution paths. The repository `quick` profile passed on 2026-10-03 (local command `python3 scripts/agent-route.py verify quick`, all listed checks exit 0). The repository `full` wrapper first exceeded its 600-second per-command timeout, so the exact Docker Go command was then run directly without that wrapper timeout: `go test -race -count=1 ./... && go vet ./...` → exit 0; `internal/server` passed in 333.309s, `internal/server/tts` passed in 3.374s, and vet completed successfully. The changed plugin tests were also run directly without race instrumentation and passed: `go test ./internal/server -run "TestCurrentTimePluginAvailableToAideAndXiaomi|TestLunarCalendarPluginAvailableToAideAndXiaomi" -count=1` → `ok aide/internal/server 1.002s`.

The local host has no `go` or `gofmt` executable; the repository's Docker test image is being used for Go verification. No browser/UI change was made, so browser QA is not applicable.

## Remaining P0 work

1. Define one capability schema for builtin, plugin, MCP, reminder and assistant-only tools, including resource boundary, read/write/execute/network needs, risk, policy mode and audit metadata.
2. Define and implement real `allow/ask/deny` semantics shared by settings, model schema, server execution and assistant execution. Approval must resume or safely resolve a pending operation rather than only hide a schema or record a suggestion.
3. Audit state-changing plugin/MCP capabilities and trusted Node execution; declarations alone cannot sandbox plugin code.
4. Design a separate execution boundary for shell and filesystem actions. Docker workspace mounting is not equivalent to a dedicated least-privilege runner.

## Release state

Test branch only. No commit, push, release, image replacement or production restart was performed.
