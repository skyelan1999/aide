# Project goals, acceptance and decision records

These are source additions dated 2026-11-13, extending existing plans, execution/delivery receipts and project token memory. **The v0.1.19.0-RC2 package does not include them.** Check source, package and running-instance identities separately. [简体中文](../project-goals-knowledge.md).

## Define and accept a goal

Open **Trajectory → Tasks and planning** in the correct project/session. Empty sessions can define goals before a model run. Add a task, optionally expanding deliverables and acceptance criteria, one per line. Use observable conditions such as a passing regression or a report containing calculation references.

Choose **Hand to aide**. The focused goal's conditions and previous operator observations enter the run context. The read-only `task_acceptance` tool returns current criteria; `criterionId` retrieves a complete observation.

After execution, inspect actual files, commands and outputs. Under **Goal and acceptance**, record each criterion as pending, passed, failed or blocked, with an actual observation and optionally a tool-receipt index. Completed execution alone does not complete a contracted goal: it awaits acceptance. Failed criteria, unapplied files or missing delivery retain the gap. All criteria must pass before the goal completes.

Unsent goal/criteria drafts are scoped to the project/session within this page and survive panel/view remounts. A full page reload or closing the page is not draft persistence.

Acceptance is an **operator observation**, not an independent automated test result. Models cannot sign off through a tool. Legacy plans without criteria keep their behavior. Revising conditions requires new acceptance; a new run invalidates previous sign-off, while preserving history. Execution/project/file-proposal/tool snapshots reject stale writes. This is not continuous monitoring of disk or external business state; reread, execute and review after external changes.

Contracted goals bind the project in which they were created. To reuse across projects, export/import the plan: import remaps IDs/dependencies and resets run/acceptance state in the destination project. Closing an unaccepted goal does not satisfy dependencies; closing an accepted goal preserves it as a completed dependency.

Limits: 10 deliverables of 200 characters; 20 criteria of 500 characters; 2,000-character observations. Keep the latest ten previous reviews per criterion and five contract-revision snapshots. Plan JSON export includes current/history records; individual prior reviews also expand in the UI.

## Project knowledge and decisions

In the same panel, open **Project knowledge and decisions**. Add facts, constraints, decisions or open questions, with a subject, content and source/version reference. A source label provides traceability, not independent verification.

Records start as **proposed**. Model proposals retain their owning session/run identities. Only explicit operator UI/API actions confirm, supersede or archive records. Confirmation records review of historical information; it grants no new authorization and proves no external effect.

For a revised decision, propose another record with the same type/subject and explicitly select which confirmed records it supersedes. Old contents, sources, confirmation timestamps and replacement relationships remain. Multiple confirmed records sharing a subject produce a conflict warning in the UI/context. Matching normalizes whitespace and case; it is not semantic contradiction detection across arbitrary prose.

Archived/superseded records leave default model reference context but remain in the UI/export. Concurrent writes reject stale snapshots and retain the unsent draft. Refreshing or modifying a different record preserves that draft for review and retry.

## Existing memory and cross-session continuity

Records extend the existing project-cache `aide/memory-tokens.json`, scoped by workspace identity, and reuse local/SSH cache synchronization. Legacy `read_memory`/`write_memory`, the 24 KiB text limit and `memory.md` projection remain. External compatibility-text edits preserve structured records; those records are not duplicated into the text projection.

New sessions receive bounded historical reference, prioritizing confirmed records and labeling proposals/conflicts. Superseded/archived entries are excluded. The context includes at most 12 whole rows and 6,000 bytes, not necessarily the complete record set. The model can search/page using `project_knowledge`: default ten, maximum twenty entries and approximately 24 KiB of record JSON per page. Reuse `nextCursor` with identical query/archive filters; changed knowledge or query invalidates it.

A cache holds up to 128 structured records, including archive/replacement history. Subject/content/source limits are 200/2,000/1,000 characters; the complete token JSON is limited to 2 MiB. New writes fail at the limit without automatic eviction. Export/back up before controlled consolidation; archiving does not release the record count.

Snapshot checks and file serialization cover one Aide process, not distributed locking/CAS against other processes or SFTP writers. SSH read failures do not fall back to same-named local state; reread after synchronization errors. Request construction uses the local working copy without waiting for SSH.

Historical records cannot override the current user or grant approval. Source-file changes do not automatically reassess a fact/decision: propose a revision and explicit replacement. Back up the complete cache before downgrading; older writers may not preserve new JSON fields.

## Interfaces

Existing authentication/origin rules apply. Record interfaces reject locked workbenches. Snapshots detect conflicts; they are not authorization tokens.

| Interface | Behavior |
| --- | --- |
| `POST /api/sessions/{id}/plan` | Optional `contract:{deliverables,criteria:[{text}]}`; the UI includes expected `workspaceId` |
| `GET /api/sessions/{id}/plan/{item}/acceptance` | Effective criteria, receipt indexes, project identity and `snapshot` |
| `PUT /api/sessions/{id}/plan/{item}/acceptance` | Revise with `snapshot` and `contract`; refused during execution |
| `POST /api/sessions/{id}/plan/{item}/acceptance` | `snapshot`, `criterionId`, `state`, `note`, optional `toolIndex`; states `pending/passed/failed/blocked` |
| `GET /api/project-knowledge` | Current project records, conflicts, `workspaceId`, `snapshot` |
| `POST /api/project-knowledge` | Expected project/snapshot and `action:propose/confirm/archive`; confirmation may explicitly name `supersedes` IDs |
| `task_acceptance` | Read-only model tool, optional `criterionId` |
| `project_knowledge` | Model `list/propose`; list supports `query/cursor/limit/includeArchived`; no model confirmation/replacement/archive |

Knowledge proposals use `kind/subject/content/source`; confirmation/archive use `id`. Both tools use existing tool switches. Plan import UI supplies `destinationWorkspaceId` to reject project changes while reading a file; legacy external import formats remain compatible.

Focused API/persistence/concurrency/context tests live in `goal_acceptance_test.go` and `project_knowledge_test.go` and join the daily basic Go gate. `TestProjectRecordsBrowser` / `scripts/browser-project-records.cjs` exercise the real UI and are also called by the existing Delivery browser gate. Actual commands, fingerprints, observations and limits are recorded in the [task ledger](../tasks/project-goals-knowledge-20261113.json). Synthetic providers/isolated fixtures do not establish paid-model, customer SSH, native hardware or production deployment acceptance.

Assistant follow-up distinguishes executor completion from goal acceptance. Revised criteria produce a new pending-acceptance notice; closed unaccepted goals are not instructions to resume work.

Follow-on capabilities: [change impact and staged workflows](project-impact-workflows.md) reuse these goals/acceptance and existing project facilities.
