# Customize Aide for a professional workflow

[简体中文](../customization.md) · [English documentation](README.md)

Aide combines AI with an IDE-style workbench. Customization is optional: start with an official release for ordinary use, or clone the source for development. A runtime image is a deployment artifact, not a complete source checkout.

Give a development agent a concrete task:

```text
Read AGENTS.md and docs/agent/WORKFLOW.md first. Inspect the branch and existing edits.
Scenario: [technical research / software development / internal workflow]
Users and objective: [who uses it; the observable result]
Inputs: [directories, formats, remote sources, permitted access]
Capabilities: [what can be reused; what must be added]
Interface: [navigation, themes, output, desktop/narrow-screen behavior]
Models/tools: [providers and services; configure secrets separately]
Acceptance: [reproducible inputs, actions and expected results]
Deployment: [OS, CPU architecture, mounts and data to preserve]
Budget/authorization: [model calls, push, release and service replacement]
Implement, test, document and package only within this scope. Report untested behavior.
```

```sh
python3 scripts/agent-route.py start my-aide --request "My scenario and acceptance criteria"
python3 scripts/agent-route.py prompt codex
```

| Change | Source entry | Validate |
| --- | --- | --- |
| Layout, menus, localization | `internal/server/web` | Real browser, themes, narrow screens, keyboard and drafts |
| Model policy | Model/profile settings and provider code | Actual selected capability, budget and failure reporting |
| Tools/plugins | [Plugin protocol](plugin-protocol.md) | Input validation, approval proposals, real results and errors |
| APIs/sources/workflows | `internal/server` | Permissions, concurrency, cancellation, persistence and restart |
| Runtime/toolchain | Dockerfiles and launchers | Architecture, non-root assets, health, backups and rollback |

Use explicit translation keys for product-owned UI. Dynamic values belong in numbered placeholders, not sentence fragments. Add English entries in `locales/en.js` with each new key. Do not translate user content, paths, custom names, schema IDs, model output or protocol tokens. Static HTML opts in through `data-i18n` attributes. Check both language directions without losing editor/input drafts. See [Localization](localization.md).

The plugin workshop can prepare, syntax-check and package extensions. Preparation is not runtime acceptance. Plugin/reference management permissions are independent and default off; confirmation rechecks workspace, permissions and target state. Trusted plugin code retains its documented execution boundary.

Use the existing `scripts/version.sh` lifecycle after implementation and verification. Preserve source identity, checksums, receipts and rollback artifacts. A merge, a tag, a package upload and a production deployment are separate actions. Never overwrite business data to upgrade a custom branch.
