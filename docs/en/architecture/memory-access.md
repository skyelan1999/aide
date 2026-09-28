# One-Way Memory Visibility and Live-Output Bridge

> Version: 0.1.11.0-RC1 · Branch: feature/permission-panel · Refs: #29 encryption / #30 cross-session tools / #31 data layering / #33 identity core
> Code: `internal/server/memory_access.go`, `internal/server/stream_broker.go`, `voice_agent.go`, `workflow.go`

aide (the AI workbench) and 小秘 (the voice secretary) share one process, but their memories and context are split under a **one-way visibility** policy. This document fixes the three core rules, the explicit permission layer, and the live-output bridge.

## 1. Three One-Way Rules (explicit user requirements, core acceptance)

1. **The secretary's memory/history is NOT shared with aide**: aide, its tools, and its sandbox commands must never read the secretary's private memory or conversation history (privacy isolation).
2. **aide's memory is visible to the secretary, read-only**: the secretary can read aide's long-term memory but **cannot write or pollute it** — the secretary side has no write method at all (compile-time guarantee).
3. **The secretary can read aide's main-session live stream**: including content **in progress** and **whatever was already produced before the run was interrupted**.

## 2. Permission Matrix

| Data zone | Path (under `/data`) | aide | secretary |
| --- | --- | --- | --- |
| aide long-term memory | `memory/core/` (`memory.md`) | read √ write √ | read √ (read-only) write × |
| secretary long-term memory | `assistant/voice-memory.json` | read × write × | read √ write √ |
| secretary history envelope | `assistant/voice-history.json` | read × write × | read √ write √ |
| aide main-session live output | in-memory (`StreamBroker`) | produces (SSE publish) | read √ (pull by session) |
| persisted session messages | `sessions/{active,archived,assistant}/` | own session | cross-session read (#30 `get_session`) |

## 3. Explicit Permission Layer (not filename coincidence)

Decisions are based on **path prefix and directory ownership**, not filenames. Even if files are renamed/moved, the policy still holds as long as they land in the right directory. Authoritative functions live in `memory_access.go`:

- `isAssistantMemoryPath(data, path)`: whether target is anywhere under `<data>/assistant/`.
- `isAideMemoryPath(data, path)`: whether target is anywhere under `<data>/memory/core/`.
- `canAccessMemory(data, caller, path, op)`: the single authority; `caller ∈ {aide, assistant}`, `op ∈ {read, write}`.
- `withinDataBase(base, target)`: normalizes via `filepath.Rel` and explicitly rejects `..` escapes (including cross-mount cases that yield a `..` prefix without an error).

Defense-in-depth points:

- **Memory tool entry**: `read_memory` / `write_memory` take no path argument (fixed to `memory/core/memory.md`); the entry still calls `canAccessMemory(aide, …)` as an assertion.
- **Workspace file tools**: `read_file/write_file/list_files` are already jailed to the workspace by `safePath()` (absolute paths and `..` rejected), so they cannot physically reach `/data`.
- **run_shell sandbox**: run_shell is an in-container bash (not blocked under `danger-full-access` mode); `shellTouchesAssistantZone()` blocks on the `/data/assistant` path prefix or the `voice-memory.json` / `voice-history.json` filenames, **regardless of sandbox mode**.

> aide's memory file was relocated from the legacy `.cache/memory.md` to `memory/core/memory.md` (#31 layout); `migrateLegacyAideMemory()` copies legacy memory once on first read/write, so nothing is lost.

## 4. Secretary Reads aide Memory (Read-Only)

`VoiceAgent.readAideMemory()` (`voice_agent.go`) reads `memory/core/memory.md` and returns a summary (truncated to 4000 chars). The type **deliberately exposes no `writeAideMemory` method** — the secretary can see but never write; a write simply cannot compile.

Injection points (two clearly separated context blocks, distinct from the secretary's private memory):

- **analyze (voice dictation / intent)**: a standalone `【aide 的长期记忆（只读参考，绝不修改；不是你自己的记忆）】` block, alongside `【你自己的长期记忆（小秘私有）】`.
- **narrate (live demo narration)**: the same read-only block appended.
- **Secretary chat (`baseSystemPrompt`, persona.go)**: the same read-only block appended to the secretary persona system prompt.

## 5. Live-Output Bridge Architecture

```mermaid
flowchart LR
    subgraph aide main session
      U[User sends message] --> E[execute / toolLoop]
      E -->|onDelta text delta| SSE[SSE publishStream]
      E -->|Publish sid,chunk,\"\"| B[(StreamBroker in-memory)]
      E -->|end: done/interrupted/failed| BF[finishLiveRun]
      BF --> B
    end
    subgraph secretary
      V[VoiceAgent] -->|getSessionLiveOutput sid| B
      V --> R[verbal summary / narration]
    end
    B -. cleared on restart .-> G[persisted messages via #30 get_session]
```

`StreamBroker` (`stream_broker.go`):

- Isolated **per sessionID**; `Publish(sessionID, chunk, status)`: `status=running` starts/resets a run; `""` appends a delta; `done/interrupted/failed` marks the terminal state.
- Keeps the latest run's full output buffer per session, capped at **100KB** (truncated beyond).
- `GetSessionLiveOutput(sessionID) → (text, status, found)`: when interrupted (`interrupted`) or failed (`failed`), **already-produced content is retained**.
- task→session mapping: `execute` calls `beginLiveRun` to register `taskID→sessionID`; `onDelta` bridges text deltas to the session dimension; `finishLiveRun` clears the mapping and marks terminal state (`cancelled→interrupted`).
- Pure in-memory: clearing on restart is expected — in-progress output should not outlive the process; completed output is already persisted, and the secretary reads it via #30 `get_session`.

**Complementary to #30 `get_session`**: `get_session` reads persisted messages; this broker covers in-progress/not-yet-persistent content and interrupted fragments. The secretary pulls via `VoiceAgent.getSessionLiveOutput(sessionID)`.

## 6. Privacy Design

- Secretary memory and history are encrypted at rest (#29); no aide tool or command in-process can reach them.
- Permissions are enforced explicitly in code, not by frontend or prompt instructions; the prompt-level "read-only" wording is only behavioral guidance for the model — real enforcement lives in `canAccessMemory` and `shellTouchesAssistantZone`.
