---
name: aide-backend
description: Implement or inspect Aide Go server, API, workflow, provider, storage, workspace, or plugin behavior. Load for backend product changes.
---

# Aide Go backend

- Start from the call chain in `docs/architecture.md`; inspect current code and adjacent tests before editing. Primary code is `cmd/aide/` and `internal/server/`.
- Preserve API compatibility, task/session state invariants, persistence semantics, and workspace identity boundaries. Check cancellation, concurrency, and error paths for long-running work.
- Prefer the narrowest package test that exercises the changed behavior. Record exact commands and outcomes; do not infer browser/model acceptance from Go tests.
- If the change crosses into web UI, runtime, or auth, ask the coordinator to route that specialist concern separately.
