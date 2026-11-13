<div align="center">

**English** · [简体中文](README.md)

# aide

### AI + IDE: one workspace for projects, models, tools and inspectable execution.

Connect local or SSH projects, use models you choose, inspect proposals and verify actual file and command results. Conversation, approvals, tool calls and execution receipts stay associated with the task.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![GitHub release](https://img.shields.io/github/v/release/skyelan1999/aide?include_prereleases)](https://github.com/skyelan1999/aide/releases) [![GitHub stars](https://img.shields.io/github/stars/skyelan1999/aide?style=flat&color=yellow)](https://github.com/skyelan1999/aide/stargazers)

[Install](docs/en/installation.md) · [User guide](docs/en/user-guide.md) · [Documentation](docs/en/README.md) · [Technical whitepaper, Chinese](docs/architecture/aide-technical-whitepaper.md)

</div>

## Work with real projects

Aide is a self-hosted engineering workbench for a trusted single-user environment. Project files, reference sources, model requests, tools and plugins share a workspace identity. Model statements, proposed changes and actual execution outcomes are tracked separately.

| Area | Current source capabilities |
| --- | --- |
| Tasks | Conversations and AI workflows, tool loops, steering/queues, pause and resume |
| Models | Provider-bound keys, parameter profiles, Chat Completions / Responses adapters, context accounting and compaction |
| Parallel work | 1X–5X and 10X capacities including the active main runtime and private workers; tools/tests run in scope, while merging candidates retains version checks and approval |
| Files and attachments | Local and SSH/SFTP workspaces, visible/read-only/writable references, Markdown editing/history, extractable documents and images |
| Large Markdown | Bounded inline excerpts plus paths, hashes and incremental reads; actual model-window limits still apply |
| Knowledge | File/conversation/code graphs, source identities and incremental refresh, original-text search and local TF-IDF retrieval |
| Plugins | Separate marketplace packages, tools, workbenches, image evidence and version-checked document interfaces |
| Approval and control | Manual/assisted review, browser access, app inspection, image-based targeting, clicks, drag, scroll and key chords |
| Assistant | Task coordination, speech and calls, subject to provider and device capabilities |
| Desktop | Configurable shortcuts and themes; optional macOS double-Shift/Command app capture with source context |
| Operations | Request traces, usage, diagnostics, backups, source builds and release verification |

Concurrency is not a proportional speed guarantee. Model review is not a passing test. See the [source-backed whitepaper](docs/architecture/aide-technical-whitepaper.md) for mechanisms and verification limits.


Source additions (2026-11-13): existing plans support deliverables and individual acceptance, while project memory supports attributed facts, constraints, decisions and questions. See [project goals and knowledge](docs/en/project-goals-knowledge.md). These additions are not in the v0.1.19.0-RC2 package.

## Start

For packaged installations, download the appropriate [Release](https://github.com/skyelan1999/aide/releases), verify its checksums and follow that version's instructions. Later `main` features are not automatically present in an older binary.

The standard source path uses Git, Docker Engine, Compose and Buildx:

```bash
git clone https://github.com/skyelan1999/aide.git
cd aide
bash scripts/install.sh --check
bash scripts/install.sh --source
```

Configure a provider/key/model, select a workspace and start a task. The default address is `https://localhost:8097`; `.env` controls the port and mounts.

- Daily start: `start.command`, `start.sh`, `start.bat` or `bash scripts/aide.sh start`; reuse a healthy existing service.
- Apply source changes: the corresponding `build` entry; `start` does not compile.
- Status/logs: `bash scripts/aide.sh status` / `bash scripts/aide.sh logs`.

Native windows and browsers can share one backend. See the [launcher contract](docs/architecture/launcher-contract.md) and [operations](HANDOVER.md) before stopping or rebuilding a working installation.

## Extend responsibly

Plugins are separately maintained and installed from the market or ZIP; updating the host does not overwrite installed plugin code. Node plugins are trusted executable code, not an adversarial sandbox. Start development with `AGENTS.md` and the [workflow](docs/agent/WORKFLOW.md).

Cloud models receive the context, images and tool results sent to their configured provider. Local roots, workspace mounts and reference write locks define accessible file scope. Keep credentials and project data out of public reports.

## Documentation and versions

[**v0.1.19.0-RC2 prerelease**](https://github.com/skyelan1999/aide/releases/tag/v0.1.19.0-RC2) was published on **2026-11-12** from **`a9f8f412`**. It includes both runtime architectures, development SDKs, checksums and validation reports. The [publication receipt](docs/reviews/2026-11-12-release-preparation/publication-rc2.json) records verification and limits. Follow the [migration guide](docs/en/release-guide-0.1.19.md); check installed packages and the running instance separately.

[Documentation center](docs/README.md) · [Architecture/API](docs/architecture.md) · [Plugin protocol](docs/plugin-protocol.md) · [Requirements](docs/PRD.md) · [Version history](version.md).

The technical whitepaper is authored as **Skye Lan**. It distinguishes source inspection, prior test receipts and unverified scenarios. Updating local documentation does not by itself publish a paper or deploy a new application.

## License

[MIT](LICENSE). Third-party components retain their own licenses. Aide is an independent project inspired by [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness); it is not an official DeepSeek product or a complete DSH/Cordis runtime implementation.


[0.1.19 usage and migration](docs/en/release-guide-0.1.19.md).
