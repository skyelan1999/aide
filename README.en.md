<div align="center">

> **Startup configuration:** `.env` is the local source of truth. `start.command` → `scripts/aide.sh` → Docker Compose, using `AIDE_PORT` (default 8097) and `COMPOSE_FILE`. Temporary review ports are not user startup entries. See [workspace mount modes](docs/workspace-paths.md).

# aide

### AI+IDE. Turn ideas into your next step.

AI and an integrated work environment · Understand information · Analyze problems · Get work done

[简体中文](README.md) · **English**

[Installation](docs/en/installation.md) · [User guide](docs/en/user-guide.md) · [Development workflow](docs/agent/WORKFLOW.md) · [GitHub](https://github.com/skyelan1999/aide)

</div>

![aide workbench with conversations, project files, and task input](docs/images/workbench-preview.jpg)

> The current baseline is **0.1.10.2 RC1**. This adds persistent memory (read/write_memory injected into the system prompt each session), model context-window presets (32K/64K/128K/200K/256K/1M one-click), a default 60 tool-call round limit (tunable 5–200 in permissions), fixed Markdown/Mermaid rendering with internal handling of relative links, trajectory export and a call-analysis view, a stacked context-preview chart, sub-agents (spawn_subagent), automatic retry and stop-loss on failures, a three-level Codex-style sandbox, a four-phase AI workflow with auto mode, five reasoning-effort levels, a voice assistant, per-tool permission toggles, configuration backup, lock screen, and persistent personas. Session management, queue/interrupt, and SSE streaming landed earlier. Screenshots use an isolated demonstration environment. See [Releases](https://github.com/skyelan1999/aide/releases) for published artifacts and their exact versions.

## Focus on your professional work

**aide = AI + IDE.** It brings project files, reference material, AI conversations, proposed changes, execution tools, and task history into one local workbench. Spend less time switching between windows and more time analyzing, making decisions, and producing results.

Use aide directly, or extend it for a specialist workflow. The repository's Agent workflow manages development, maintenance, and customization; it is not an automatic product generator and does not replace professional judgment.

- **Work with real projects.** Browse local or SSH/SFTP workspaces, edit text, and attach reference files.
- **Inspect the process.** Review plans, tool calls, proposed file changes, and suggested commands before acting.
- **Keep environments portable.** Docker provides Go, Python, Node.js, and Git. Source, images, working files, and persistent data are managed separately.

Data is persisted locally. When using a cloud model, task content, context, and tool results are sent to the provider you configure. aide is an independent project inspired by DeepSeek Harness, not an official DeepSeek product or a full DSH/Cordis runtime.

## What you can do

| Capability | How it works | Boundary |
| --- | --- | --- |
| AI chat and workflows | Chat, or follow Plan → Propose → Review | Model review is not a passing test; responses appear by step |
| Files and Markdown | Browse, edit, preview, open in a new tab, and attach files | Text/size limits apply; saves check file hashes |
| Workspaces and references | Local, SSH/SFTP, Skill directories, links, FTP/FTPS/SMB | MCP registration is not a working MCP connection |
| Models and strategies | Multiple model IDs, parameter profiles, automatic/manual routing | Models share the current provider connection |
| Trajectory, search, compaction | Inspect events, search conversation history, summarize old context | Compaction is summarization, not lossless compression or ZIP |
| Usage and cost | Heatmap, daily details, rate snapshots, provider balance | Priced, estimated, and unpriced usage differ; this is not an invoice |
| Command panel | Run, inspect, and cancel individual shell commands | No PTY or persistent shell session |
| Plugins | Upload, enable, and call tools with executable handlers; built-in draw.io diagram tool | Plugins are trusted code, not a security sandbox |
| Permissions, voice & personalization | Three-level sandbox (read-only/workspace-write/full-access), per-tool toggles, lock screen; voice transcription, persistent personas; config backup export/import | The sandbox bounds container privileges, not malicious plugins; speech recognition runs in the browser |
| Appearance and language | Professional/Classic palettes; light/dark/system; Chinese/English | Browser-local preferences; language does not translate your content |

## Quick start

Install Git and Docker Engine/Desktop with Compose v2. Image checksum validation also needs Python 3. Host Go/npm are not needed for ordinary use.

```bash
git clone https://github.com/skyelan1999/aide.git
cd aide
bash scripts/install.sh --check
bash scripts/install.sh --source
```

The installer creates a configuration only when `.env` does not exist, prepares a reference directory, builds the source, and starts the service. Existing settings and data volumes are preserved. First-time downloads need internet access.

For a published ARM64 image, download the matching source, image archive, and `SHA256SUMS`, then use `scripts/install.sh --image ARCHIVE`. Follow the exact versioned commands in [Installation](docs/en/installation.md); do not mix source and image versions.

Edit `.env` to select existing directories:

```dotenv
AIDE_PORT=8097
AIDE_WORKSPACE=/absolute/path/to/project
AIDE_CONTEXT=/absolute/path/to/references
AIDE_LOCAL_ROOT=/absolute/path/to/projects
```

`/workspace` and `/local` are writable; `/context` is read-only. The raw Compose default for `/local` is your entire HOME. The first-run installer narrows this to the repository. Use a scope appropriate to your project.

Open `http://127.0.0.1:8097`. The startup script opens a local token login automatically when a desktop opener is available. The local access token is separate from your model API key; do not share either.

## Your first task

1. Open **Model settings** and configure a Chat Completions-compatible provider, model ID, and key.
2. Confirm the workspace. Open a file and select **Attach to task**.
3. Choose a model and profile under **Strategy**.
4. Use **Chat** to understand the project or **AI workflow** to propose changes.
5. Review the proposed files before applying them. Existing-file changes require the attachment snapshot and conflict checks.
6. Review suggested commands and run them separately in the command panel. Check actual results.

Choose **Settings → Language → 中文 / English** in a build with language support. The login screen and standalone file view also expose a language selector. Switching language preserves task drafts and editor contents. Model responses, file names, custom profiles, plugins, terminal output, and provider error details retain their original content.

## Extend aide

Describe your specialist task, data sources, tools, permissions, and acceptance criteria. Let your development AI read [AGENTS.md](AGENTS.md) and follow the shared route:

**Requirements → Design → Implementation → Verification → Documentation → Cleanup → Release**

```bash
python3 scripts/agent-route.py start my-feature --request "My scenario and acceptance criteria"
python3 scripts/agent-route.py prompt codex
python3 scripts/agent-route.py verify quick
```

See [Customization](docs/customization.md) and [Client adapters](docs/agent/ADAPTERS.md). Local instruction files cannot force external clients to comply; confirm that each client actually loaded them.

## Operations and limits

```bash
bash scripts/aide.sh status
bash scripts/aide.sh logs
bash scripts/aide.sh stop
```

Go embeds the frontend; rebuilding the image is necessary after source changes. Images do not contain mounted project files, API keys, or session data. Back up data volumes separately.

This is a trusted, single-user local workbench, not public multi-user hosting. AI read tools may read beyond explicitly attached files. File application is atomic per file, not a multi-file transaction. Plugins can use Node capabilities. Per-token SSE streaming is supported (chat mode updates live). Full MCP, PTY, and complete DSH compatibility are not implemented. Token estimates and compaction can lose precision or detail.

[Documentation index](docs/en/README.md) · [Architecture](docs/architecture.md) · [Operations](HANDOVER.md) · [Plugin protocol](docs/plugin-protocol.md) · [Requirements](docs/PRD.md)

## License and acknowledgements

[MIT License](LICENSE). Bundled third-party components retain their licenses. Thanks to [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) for design inspiration.
