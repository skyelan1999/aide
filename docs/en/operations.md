# Operations, verification and release handoff

[Canonical operations record](../../HANDOVER.md) · [English documentation](README.md)

Use the repository's `AGENTS.md`, `docs/agent/WORKFLOW.md` and `docs/agent/router.json` for development gates. Keep a task ledger with the requested scope, acceptance, tested source identity, remaining limitations, running service and rollback state. The workflow is an operator/agent contract; it is not proof that an external client loaded it.

```sh
python3 scripts/agent-route.py check
python3 scripts/agent-route.py verify quick
python3 scripts/agent-route.py verify full
python3 scripts/agent-route.py audit
```

Quick checks include syntax and focused frontend/API contracts. Full adds isolated Docker Go race/vet and runtime-related tests. UI changes also require real browser acceptance. A component test does not establish rendered behavior, paid model availability, remote-server operation or hardware/audio performance.

Back up source edits, workspace files, application `/data`, credential configuration and image archives separately. `/workspace` holds working files, `/context` normally holds read-only references, and `/data` holds sessions and credentials. Do not place secrets or business data in Git. Do not use `git clean -fdx` or `docker compose down -v` as routine maintenance.

Before starting or replacing a service, identify `.env`, Compose project, port, image and mounted data. Reuse the installation's own start entry. An isolated acceptance port is not the user's deployment entry. Check health, authenticated API behavior and source identity rather than relying only on a corner version label.

Source merge, push, version bump, tag, release upload and production deployment are separately recorded. Version operations use `scripts/version.sh` on main. The delivery workflow builds native ARM64 and AMD64 candidates, checks package identities and publishes only through its explicit release gate/environment. Never substitute a private development image for a public candidate. Release notes should state concrete behavior, validation and limits in both languages.

For code rollback, prefer a reviewed `git revert` and fresh verification. Test data recovery on a separate copy before switching. Do not roll an old image onto the only copy of newer data without checking compatibility. Retain the previous validated image and data backup until the replacement is accepted.

Historical reports under reviews/tasks/archive preserve their original dates, failures and untested ranges. Translation does not turn old receipts into current release evidence. See [Docker maintenance](docker-maintenance.md), [runtime fallback](runtime-bundle.md) and [installation](installation.md) for operator procedures.
