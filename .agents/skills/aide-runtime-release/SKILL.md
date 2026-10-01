---
name: aide-runtime-release
description: Change Aide startup, Docker/Compose, port selection, installation, packaging, software upgrades, or release assets. Load for host/runtime and distribution work.
---

# Aide runtime and release

- Trace the actual path through `start.command`, `scripts/aide.sh`, install/host scripts, Compose, and image metadata. Source checkout and release-image modes may have different upgrade capabilities; preserve both when applicable.
- Keep workspace/session/settings data mounts stable across image switches. Treat port, health check, token bootstrap, architecture, rollback, and update-slot state as one lifecycle.
- Use only the version/build/release machinery already documented in `AGENTS.md`, `HANDOVER.md`, and the relevant `docs/architecture/` page. Do not invent tags or claim an asset exists before checking it.
- Run syntax and focused lifecycle checks first. Build, push, publish, or restart only within the user's authorization; record each as a separate outcome.
