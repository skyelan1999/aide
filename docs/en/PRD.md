# Product requirements and acceptance boundaries

[Canonical requirement history](../PRD.md) · [English documentation](README.md)

Aide is a single-user, local-first AI development workbench with controlled file sources, model/provider configuration, auditable task execution and optional trusted extensions. The current English product contract is organized below; historical FR identifiers and dated acceptance states remain in the original requirement ledger rather than being renumbered or reclassified as passed.

## Required behavior

| Area | Product contract |
| --- | --- |
| Sessions | Persist conversations, titles, run/stage/tool records and workspace association. Preserve drafts during switching and language changes. |
| Models | Separate provider credentials, model capabilities, parameter profiles, routing decisions, usage and pricing. Do not equate selecting a text model with producing native media. |
| Context | Preview request budgets, reserve output, reject over-budget requests appropriately and compact history using validated snapshots. Keep estimates distinct from upstream usage. |
| Execution | Support ordinary conversation and workflows, cancellation, pause/resume where checkpoints permit, queueing and explicit approval. Proposed writes/commands must not be reported as executed. |
| Files/sources | Enforce source-relative paths, permissions, visibility, identity and hash conflicts across local/SSH access. Preserve read-only references. |
| Knowledge | Provide bounded index/search, source-aware excerpts and evidence provenance. Clearly report incomplete coverage and unresolved static relationships. |
| Plugins | Validate package/lifecycle contracts, actual tool ownership, dependencies and human-confirmed management. Trusted Node execution is not a sandbox. |
| Assistant | Coordinate explicitly assigned work with independent authentication, microphone, message and management permissions. Treat uncertain state as uncertain. |
| Communication | Use device identities and separate invitation consent for contacts/groups/calls/files. Do not mistake lookup codes for authentication. |
| Interface | Provide desktop/narrow-screen layouts, keyboard-accessible controls, independent palette and light/dark preferences, reduced motion and explicit Chinese/English product text. |
| Documents | Make installation, operation, customization, extension and localization understandable in English without changing historical evidence. |
| Delivery | Identify source, tag, package, platform and runtime separately. Build/verify native artifacts and preserve data/backups across upgrades. |

## Historical identifier map

FR-01–24 cover sessions, persistence, filesystem/command boundaries, model configuration and streaming. FR-25–33 cover conversation/workflow proposals, attachments, approvals and tool loops. FR-34–48 cover layout, status, file selection, launchers, packaging and recovery. FR-49–60 cover theme/palette/preferences and settings. FR-61–75 cover profiles, routing, versions, multiple models, context/usage and plugins. FR-76–84 cover workspaces, SSH, remote files, source registries and browsing. FR-85 is reserved; FR-86–94 cover Markdown, model selection, summaries, file views, usage, trajectory, search, compaction and context preview. FR-95–100 cover appearance, About, cross-client workflow, documentation, professional-workbench positioning and first-install checks.

Historical tables include features that were missing or partially verified at their original dates. They must not be read as today's complete capability verdict. For example, an old PTY/MCP/preview statement can predate later implementation. Check current source and the specific test/release receipt before making an availability claim.

## Non-goals and evidence limits

Aide does not provide malicious-plugin OS isolation, a guarantee of globally complete knowledge retrieval, proof that a model read every citation, automatic cross-device reachability, or universal provider/model availability. A generated document proposal is not a saved business artifact. A static call graph is not runtime execution. A translated page is not a certified accessibility/conformance audit. Old tests are not receipts for a new release.

Acceptance must record reproducible inputs, actual operations, observed results, remaining uncertainty and exact source/package identity. The public product is independent of private research branches and independently distributed plugins.
