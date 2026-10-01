---
name: aide-verification-docs
description: Validate Aide changes, reproduce bugs, update implementation docs, maintain task evidence, or prepare handoff. Load for QA and documentation tasks.
---

# Aide verification and documentation

- Derive checks from the acceptance criteria and changed call paths; select focused tests first, then the repository quick/full profile as appropriate.
- Keep evidence factual: record environment, command, result, and limits. Static inspection is not runtime acceptance; a rendered page is not proof of persistence or API behavior.
- Update the current docs to match implemented behavior, link sources, and distinguish available capability from tested capability. Run `python3 scripts/check_docs.py` after Markdown edits.
- UI claims require actual browser interaction. If a capability or environment is unavailable, report `NOT_RUN` and name the next verification owner/action.
