# Architecture and API guide

[简体中文](../architecture.md) · [English documentation](README.md)

Aide is a Go HTTP service with an embedded native JavaScript/CSS browser UI and a container runtime. Model steps run in goroutines; SSE streams incremental output while persisted session snapshots provide final-state recovery. The Node plugin host executes trusted extensions and is not an independent security sandbox.

```mermaid
flowchart TB
  Browser[Conversations, files, settings and trajectory] --> API[Go HTTP API: authentication and origin checks]
  API --> Runs[Task engine and context budgets]
  API --> Workspace[Workspace identity: local or SSH]
  API --> Storage[Sessions, settings, sources and usage]
  Runs --> Provider[Configured model provider]
  Runs --> Tools[Built-in tools and trusted Node plugins]
  Tools --> Proposal[File and command proposals]
  Proposal --> Approval[User approval]
  Approval --> Workspace
  Workspace --> Mounts[/workspace, /context, /local]
  Storage --> Data[/data persistent storage]
```

## Source map

Names without a prefix below are in `internal/server/`.

| Module | Responsibility |
| --- | --- |
| `cmd/aide/main.go`, `server.go` | Startup, routes, authentication, settings, sessions, pricing and build identity |
| `workflow.go`, `context.go`, `provider.go` | Task/tool loop, proposals, compaction, context preview/budgets, provider streaming and usage |
| `profiles.go` | Built-in/custom parameter profiles and routing policy |
| `workspace_config.go`, `ssh_session.go` | Workspace identity, local mappings and SSH/SFTP lifecycle |
| `files.go`, `sources.go`, `mcp_source.go` | File/source boundaries, conflict checks and constrained MCP transports |
| `knowledge_map.go`, `knowledge_updates.go`, `knowledge_sources.go` | Bounded multi-source indexes, identity rechecks, deltas and caches |
| `knowledge_code.go`, `code_analysis/` | Go, JavaScript and Python ASTs and static call candidates |
| `knowledge_documents.go`, `document_analysis/` | Source extraction, keyword retrieval, optional hybrid retrieval and source fingerprints |
| `command.go` | Local/SSH command streams, cancellation and interactive PTY bytes |
| `persona.go`, `voice_agent.go` | Persona configuration and encrypted assistant conversation records |
| `generated_documents.go`, `document_proposals.go` | Configured document destinations and source-bound proposals |
| `management.go`, `plugins.go`, `plugin_host.js` | Plugin/reference management, human confirmation, lifecycle and tool ownership |
| `assistant_watch.go`, `assistant_agent.go` | Assistant task follow-up, actions and separately authorized review |
| `message_center.go`, `internal/messaging/` | Contacts, groups, invitations, messaging and resumable transfers |
| `tts_catalog.go`, `tts/` | Voice catalogs and synthesis adapters |
| `web/settings-init.js`, `web/i18n.js`, `web/locales/en.js` | Initial preferences, explicit product translations and catalog |
| `web/app.js`, `web/starmap*`, `web/*preview*` | Main UI, independent knowledge map and bounded viewers |

Product model routing (`profiles.json`, `routing-policy.json`) is distinct from the development-agent workflow (`AGENTS.md`, workflow/router documents).

## Data, identity and permissions

Sessions are stored in `/data/session-<id>.json`; settings, credentials, source registry and usage are separate persisted records. Source registration is in `/data/config/sources.json`; browser appearance/language live in `localStorage['aide.ui']`. Runtime layout details are documented in [Data layout](architecture/data-layout.md).

File APIs enforce root/source bounds and workspace identity. Editor saves and old proposals must pass identity/hash checks. Applying multiple files can partially succeed even though individual replacements are atomic. An enabled writable reference is still subject to filesystem permissions. Commands and trusted plugins can reach the directories available to their container user; UI management flags do not create an OS sandbox.

Context preflight budgets history and reserved output against the selected model window. Compaction creates a structured summary plus recent history. It commits only after summary generation and snapshot validation; cancellation or concurrent changes prevent stale replacement. Session-scoped progress survives switching away and back. Compaction does not promise deletion of old run records or disk shrinkage.

Knowledge indexes are bounded observations, not complete version control. Source identity and content fingerprints accompany excerpts. Static name candidates, optional type bindings, document extraction and execution traces are different evidence classes. MCP catalog indexing does not run a tool. See the English [knowledge-map guide](knowledge-map.md).

## API families

All paths below are under the authenticated `/api` namespace unless explicitly public. The implementation remains authoritative for request fields, permissions and bounds.

| Family | Purpose |
| --- | --- |
| `/config`, `/profiles`, `/workspace-config` | Runtime configuration, profiles and workspace binding |
| `/sessions`, `/sessions/{id}/runs/{run}/events` | Conversation/task state and SSE |
| `/sources` | Registered local, reference and remote sources |
| `/knowledge-map/*`, `/knowledge-map/embedding-policy` | Indexes, document retrieval, history and opt-in embedding policy |
| `/capabilities`, `/capabilities/check` | Capability metadata and explicit scoped checks |
| `/plugins` and plugin lifecycle endpoints | Trusted extension installation, settings, activation and upgrades |
| `/file`, `/file/raw`, `/file/download`, `/file/archive`, `/file/extract`, `/file/rename` | Source-aware files, downloads and bounded archives |
| `/token-pricing` | Usage pricing configuration; estimates are not upstream receipts |
| `/tts/*` | Separate synthesis/read-aloud services |
| `/debug/*` | Explicitly authorized diagnostics; see [Debug API](debug-api.md) |

## UI, assistant and communication

Palette and light/dark/system mode are independent browser preferences. Translation uses explicit keys; content, filenames, custom names and provider output remain untouched. Static pages and dynamic controls share the same catalog. Missing keys fall back to their original source text. Language switching must not discard drafts or change protocol values.

The assistant is page-owned coordination with separate microphone, message-access and management authorization. Native realtime audio, browser recognition and TTS are independent paths; see [Voice calls](assistant-voice-call.md). Persona/assistant encryption and password controls remain distinct from work-task execution.

Messages use cryptographic device identities; short lookup codes are discovery aids. Contacts, group membership, calls and files have independent consent. LAN discovery, Internet transport, encryption, resumable transfers and NAT reachability must be validated separately.

## Extension and delivery boundaries

Use [Plugin protocol](plugin-protocol.md) for Node/Python tools and isolated workbench UI contracts. Package validation does not prove arbitrary plugin behavior. Model/provider compatibility must be tested for the requested capability, and generated media require actual artifacts.

The slim runtime and separately delivered development toolbox have different contents. STEP preview is supplied by the public STEP Viewer plugin; removing it from the host does not remove unrelated DXF rendering. Official package identity is verified by tag, source commit, checksums, imported image metadata and runtime checks. A local development branch or old screenshot does not certify a public release.
