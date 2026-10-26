# Plugin protocol and extension contracts

[简体中文](../plugin-protocol.md) · [English documentation](README.md)

A plugin is trusted JavaScript loaded by `internal/server/plugin_host.js` in the Aide container. Its module exports an object with `apply(ctx)`. The DSH/Cordis resemblance is a shape-compatible subset, not complete runtime compatibility, service injection or arbitrary desktop/network authorization. Preserve protocol identifiers and JSON fields in their original form regardless of UI language.

## Package and registration

Installed plugins live under `plugins/<id>/` with a manifest, entry module and optional resources; `plugins/registry.json` records registration. The authenticated host validates package structure, paths, CRC, entry size and plugin identity. Enabling/loading executes trusted code as the container's Aide user; this is not a malicious-code sandbox. Validate dependencies and bundled licenses before distributing a package. A declared tool, panel or capability is not proof its implementation works.

```js
'use strict';
module.exports = {
  name: 'hello-aide',
  apply(ctx) {
    ctx.logger.info('hello-aide loaded');
    ctx.tool({name: 'hello', description: 'Greet the user'});
    ctx.slot({id: 'hello-panel', name: 'Greeting panel'});
  },
};
```

Read the canonical manifest/setting schema and host source for all supported fields. Existing tool names, action values, service names and setting IDs are machine contracts, not translation keys. User-facing labels/descriptions can be localized without renaming them.

## Tool handlers

| API | Contract |
| --- | --- |
| `api.readFile(rel)`, `api.listFiles(rel)` | Direct bounded access to the current local workspace; absolute paths, traversal and symlink escape are rejected. SSH does not silently fall back to a local directory. |
| `api.proposeWrite(rel, content)`, `api.proposeCommand(cmd)` | Return proposals for the host approval flow. Do not claim the write/command already occurred. |
| `api.runPython(script, input?, options?)` | Plugin-local Python execution with JSON stdin/stdout; default 60 s, maximum 280 s, stdout up to 8 MiB and stderr up to 64 KiB. No shell argument execution. |
| `api.log(...)` | Host logging; do not log credentials. |

Handler calls have bounded lifetimes (up to 300 s). Built-in `run_shell` uses the configured sandbox mode; `write_file` creates a proposal. Tool results return to the model as tool messages. Existing file-attachment/source permissions still apply.

## Services and lifecycle

Modules can declare `provides` and `requires`, each with up to 64 unique service names. Synchronous dependency-ordered `apply(ctx)` registers real values using `ctx.provide` and consumes them using `ctx.consume`. Undeclared consumption, missing/duplicate providers, cycles, unregistered declared services and duplicate tool names fail the surface. Cleanup runs in reverse order.

Short-lived calls reassemble the current enabled dependency set and check actual tool ownership. Disabling a dependency cannot be bypassed by a stale tool schema. This is not cross-process service injection, asynchronous `apply`, or full Cordis compatibility. Daemons retain their separately documented lifecycle; see [Daemon protocol](plugins/daemon-protocol.md).

## Workbench and source access

Workbench UI uses its declared controlled bridge and capability allowlist; an iframe does not gain arbitrary HTTP, credentials or code-execution authority from showing a panel. Read-only source snapshots use `api.sources(request)` during admitted short-lived calls. Operations include `sources`, `browse`, `stage` and local-only `localRoot`. Source IDs are `workspace`, `context` or `source:<id>`. The opaque `workspaceKey` isolates caches but does not grant access or expose credentials.

Staging is bounded: 8 MiB per file, 64 MiB per call, 5,000 entries per side/list, depth 32 and 90 s per source request, or lower requested budgets. Reserved paths, links, special files and unparseable remote entries are rejected/excluded according to the source contract. A failed/over-budget snapshot is not returned as a complete directory. Snapshot provenance records exclusions and that original modification times were not preserved. Temporary roots are cleaned after return/cancellation/error. Sources and workspace identities are rechecked before and after work. This does not guarantee a cross-file atomic snapshot of a changing directory.

`api.runPython(..., {sourceSnapshot:true})` explicitly permits supported source-snapshot handling for a remote workspace. It does not redefine `workspaceRoot` or make ordinary read/list helpers fall back locally.

## Management and updates

The workshop can prepare a downloadable ZIP and a management proposal without executing generated code. Aide/assistant plugin/reference management flags default off. Human confirmation rechecks role, workspace, target version and package fingerprint. Existing reference visibility/write locks remain in force; new references default read-only. Assistant task-review delegation does not auto-approve management operations.

Online upgrade accepts a local ZIP or supported public HTTPS ZIP URL. It preserves settings, enabled state and recoverable backups, rejects known numeric downgrades, and requires matching plugin IDs. Download/CRC/integrity validation is not a publisher signature. Bounds include a 24 MiB download, 64 MiB expanded content, 256 entries, 16 MiB per file and a 256 KiB entry module; downloads do not install npm/pip dependencies automatically.

The host waits for active tool resources, validates a staged candidate and its enabled dependencies, writes a recovery journal, then commits the directory/registry switch. An inactive plugin stays inactive. A backup directory retains the old tree and registry snapshot. Startup recovers incomplete transactions; a committed version remains committed. Workspace/settings/version changes reject stale requests; only one upgrade runs per instance. A disconnected browser must refresh to inspect the result.

Filesystem/registry rollback does not undo external device/network side effects of trusted plugin code. There is no blanket OS isolation guarantee and no current automatic manual-rollback UI. Stop the service and verify the backup registry/tree before any manual recovery.

## Skills and distribution

Skill-library search uses bounded public repository metadata, fixed commits and Git blob hashes. Preparing/registering a cached skill requires the appropriate reference-management permission and human confirmation. Skills do not execute during download; their text or `allowed-tools` declarations do not grant permissions. Cached skill versions retain licenses and source receipts.

Keep independently distributed plugin internals, proposals, assets and release notes in their owning repositories. Public host changes should contain only generic host contracts, required code and neutral tests. Do not bundle installed/private plugin payloads into Aide's public runtime or source artifact.
