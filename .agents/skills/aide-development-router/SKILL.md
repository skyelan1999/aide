---
name: aide-development-router
description: Route Aide repository development requests to the smallest useful specialist skill and agent team. Use at task intake, resume, or when dividing work across agents.
---

# aide development router

Use this skill only as the entry point. Keep project policy in `AGENTS.md` and `docs/agent/WORKFLOW.md`; do not duplicate the full release or verification process here.

## Intake

1. From the repository root, read `AGENTS.md`, `docs/agent/WORKFLOW.md`, `docs/agent/router.json`, then inspect Git status and the applicable `docs/tasks/<id>.json`.
2. Run `python3 scripts/agent-route.py route --request "<original request>"` to select specialist skills. Treat its output as routing advice, not permission to spawn agents, edit outside scope, push, publish, or stop services.
3. Assign one primary skill to the implementation owner. Add a second specialist only for a distinct, necessary concern. Keep a coordinator responsible for shared task record, integration, and final verification.
4. If routes overlap, split by non-overlapping files or work products. Avoid multiple agents editing the same file concurrently. If no route matches, stay with the coordinator and inspect before delegating.

## Agent handoff

Each assignment must include the user request, task ID, acceptance criteria, allowed paths, baseline Git status, expected deliverable, and evidence required. Specialists must report changed files, commands actually run, results, and unresolved risks. The coordinator owns merge/integration and the repository workflow stages; an agent's report is not verification evidence until checked.

## Client handling

Do not assume a client automatically loads this directory. Use `python3 scripts/agent-route.py prompt <client> --request "<original request>"` to create a portable bootstrap for supported clients, or include the selected skill output from `route`. Never send prompts to an external service automatically.
