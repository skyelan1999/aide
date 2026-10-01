---
name: aide-frontend
description: Implement or inspect Aide browser UI, settings, styles, accessibility, localization, and file viewers. Load for frontend or visual interaction changes.
---

# Aide web UI

- Read the relevant UI section in `docs/architecture.md`; source is `internal/server/web/` (native JavaScript/CSS, not a separate frontend build system).
- Trace existing state, persistence, localization, keyboard behavior, and responsive layout before editing. Keep shared components and translations consistent across supported locales.
- Run focused `node --check` or repository UI scripts for changed files. For visible UI changes, open the actual page and verify the interaction at relevant sizes/themes; if unavailable, report `NOT_RUN`.
- Go API or security changes belong to their matching specialist; coordinate API shapes explicitly rather than silently inventing them.
