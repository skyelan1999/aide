# Development workflow and AI clients

[Canonical workflow](../agent/WORKFLOW.md) · [Client adapter reference](../agent/ADAPTERS.md) · [English documentation](README.md)

Read `AGENTS.md`, `docs/agent/WORKFLOW.md` and `docs/agent/router.json` before changing the repository. Inspect the branch and existing edits. The router suggests the smallest relevant specialist skill; it does not itself create agents, authorize paid calls, restart a service or publish anything.

```sh
python3 scripts/agent-route.py route --request "Requested behavior and acceptance criteria"
python3 scripts/agent-route.py start task-id --request "Original request"
python3 scripts/agent-route.py prompt codex
```

Resume an existing task rather than overwriting its ledger. Record scope and observable acceptance before implementation. Keep requirements, design, implementation, verification, documentation, cleanup and release as separate stages. Valid states are `pending`, `in_progress`, `pass`, `blocked` and `not_run`. A planned check cannot be recorded as passed.

Use existing capabilities before adding new ones. Keep credentials, private plugins and business data out of public issues, logs, commits and packages. Preserve others' edits. Untracked files are not automatically garbage; classify them, retain licenses/history and make cleanup recoverable. Never use broad `git clean -fdx` for routine cleanup.

For UI work, inspect real desktop/narrow-screen pages and keyboard/focus behavior. For backend state changes, exercise permissions, concurrency, cancellation and persistence as appropriate. Mock/provider/browser/hardware/release tests establish different facts. Report an unavailable test as not run with its concrete limitation.

```sh
python3 scripts/agent-route.py verify quick
python3 scripts/agent-route.py verify full
python3 scripts/check_docs.py
```

The full gate runs isolated Go race tests and vet plus runtime checks. Source snapshots avoid testing mutable shared mounts. Evidence logs remain under ignored `.agent-state/`; durable, sanitized conclusions belong in the owning task/review. Do not upload raw runtime configuration or tokens.

Client adapters differ in filesystem, browser, shell and tool availability. The generated prompt provides a portable handoff to Codex, Claude, DeepSeek, Doubao or WorkBuddy. It does not guarantee a remote agent automatically loads repository rules. Do not send another client a message or create external work merely because the router suggested a role.

Before release, finish required checks, inspect staged files and confirm exact authorization. Use `scripts/version.sh` on main for version/tag changes. Follow branch protection and CI/review requirements; merge, push, release upload and deployment are distinct recorded steps. Preserve rollback images and data copies. Never substitute old receipts for the current source fingerprint.
