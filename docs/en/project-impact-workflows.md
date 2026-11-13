# Change impact and staged business workflows

These source additions dated 2026-11-13 reuse the existing knowledge map, plans, plugin tools, file approvals and [goal acceptance](project-goals-knowledge.md). **Published v0.1.19.0-RC2 packages do not contain them.** No automatic runtime upgrade, plugin installation or publication is performed. [简体中文](../project-impact-workflows.md).

## Start here

Choose the correct project/session and open **Trajectory → Tasks and planning**.

- To find what else a changed parameter may affect, expand **Change impact analysis**.
- To deliver software maintenance or engineering work in steps, expand **Reusable business workflows**.
- For an existing workflow, follow its progress and **Next** prompt. Start or review the current stage rather than creating another workflow.

Empty sessions work too. Unsubmitted drafts remain scoped to this project/session within the page; closing or fully reloading the page is not persistent draft storage.

## Review change impact

Describe the change, search/select the directly changed project files, then analyze. For example, increase design pressure from 10 to 12 MPa and review calculations/reports. Refresh if a file is missing; manual paths are an optional folded fallback, and files still need to be indexed.

Expand the saved analysis and inspect file trails, original relation direction, locations and evidence. Choose **Must change** for confirmed edits, **Needs review** for uncertainty, and **Not affected for now** only after checking. Mark an item handled or blocked and record actual evidence. Keep the latest ten previous reviews. The actual JSON export includes analyses, relations and observations.

Direct seeds start as must-change suggestions; related candidates start as review suggestions. File/symbol relations are collapsed to files and traversed in either direction, at most three hops/80 files, excluding structural containment. Traversing backwards never changes original arrows or evidence grades. Limits: eight seeds, twenty analyses per session, 2,000-character description/observation; 2 MiB of impact JSON per session. Export and continue in a new session at the count/size limit; exporting does not clear records. The picker offers at most 16,000 indexed paths / 1 MiB of path data and displays forty matching choices at a time.

**An omitted file is not proven unaffected.** Existing source visibility, document-prefix/binary-name limits, static parsing, progressive discovery and read budgets remain. This is not an engineering calculator or runtime trace. Drawings, calculations and reports can appear only when indexed relations connect them. Coverage/read failures and truncation remain visible.

Analyses bind project and file/symbol index revisions. Changed file content, source identity, relations or coverage make old analyses historical and reject further reviews: analyze again. Ordinary conversation messages and refresh-cycle counters do not constitute file changes. Remote caches retain their existing policy; this is not continuous disk, customer SSH or external-system monitoring.

## Deliver a workflow

1. Choose **Software maintenance** or **Engineering delivery**, then enter the specific goal/scope.
2. Expand stages to preview deliverables/checks. Configure professional plugin tools only when needed, using searchable names/capabilities and checkboxes. You can start without selecting any. Existing tool switches and approvals still apply.
3. Create the stage goals. Progress and a next-step action identify the current stage and locate the first goal.
4. Start that stage. Models/plugins use the existing execution channel; file proposals follow existing approval policy.
5. Inspect actual results and record each acceptance check. Only accepted predecessors unlock the next stage. Expand **Stage inputs and provenance** to inspect source runs, text, file digests and tool receipts.
6. A failed later stage preserves completed predecessors. Retry the incomplete stage from its goal. A paused run can continue through its original checkpoint/recovery UI. Unknown external effects require the existing recovery review, never an automatic replay through a fresh stage.

| Scenario | Stages |
| --- | --- |
| Software maintenance | Understand → Reproduce/impact → Implement → Regress → Review/deliver |
| Engineering delivery | Review inputs → Calculate/analyze → Produce reports/drawings → Check → Revise/deliver |

Each stage has deliverables, two editable criteria and predecessor dependencies. Templates prove neither calculation accuracy, test success, saved files nor external PR/publication success. They do not sign off for users. Professional capabilities depend on installed, enabled, configured plugins/models. Existing PW visual workflows and their signed runtime retain their own capabilities/permission boundaries; these recipes reuse ordinary plans/plugin execution without changing that signed engine.

Selected professional tools bind package versions/tool definitions and are checked at stage admission and dispatch. Acceptance requires actual receipts for those tools; a receipt alone does not prove correctness. If tools are disabled/upgraded, inspect the plugin and explicitly save the stage's tool configuration. Changed configuration requires a fresh stage run/acceptance and invalidates previous downstream sign-off.

A stage receives **accepted predecessor snapshots**, not live filesystem verification: source goal/run, bounded final completed-step text, digests of applied file proposals and bounded tool receipts. At most eight direct inputs; each keeps 4,000 text characters, forty file digests and ten 1,000-character receipt excerpts. Truncation is explicit; reread original files/runs for omitted data.

Changed predecessor conditions, reviews or runs invalidate downstream input/acceptance transitively. Later reacceptance never silently reaccepts old downstream results. Existing plan export/import preserves reusable stage metadata while remapping goal/group/dependency/project identities and resetting runs/sign-off. Import binds selected tools to currently available definitions. Flows must retain acceptance contracts and complete acyclic dependencies.

## Model reads

`change_impact` is read-only within the current session/project. Read summaries, then page with `analysisId/snapshot/offset`: at most ten items/~24 KiB of record JSON, with 400-character observation excerpts. It cannot classify/sign off and does not freshly verify source state.

`workflow_inputs` selects a source with zero-based `inputIndex`. Follow `nextInputIndex` and `nextOutputOffset/nextFileOffset/nextReceiptOffset`. Each page returns at most 1,000 text characters, five files and two receipts, capped at 64 KiB. Pagination cannot restore content already truncated in storage; inspect original runs/files.

Both tools use existing switches. Goal criteria, recent impact-list identities and predecessor summaries enter existing planning context as bounded historical reference, never authorization.

## Interfaces and storage

| Interface | Behavior |
| --- | --- |
| `GET /api/sessions/{id}/impacts` | Current-project analyses, stale/complete flags, seed-file picker, revision and write snapshot |
| `POST /api/sessions/{id}/impacts` | `workspaceId/snapshot` with `action:analyze/change/paths`, or `action:review/id/itemId/classification/state/note` |
| `GET /api/sessions/{id}/plan/flows?language=en` | Localized recipe preview, enabled tools/readable labels and plan snapshot |
| `POST /api/sessions/{id}/plan/flows` | `workspaceId/snapshot/recipe/brief/language/stageTools`; recipes `software/engineering` |
| Same POST, `action:configure` | `workspaceId/snapshot/itemId/tools`; explicit stage-tool replacement, refused during execution/approval |
| Existing plan export/import, run and acceptance endpoints | Reuse existing persistence, identity remapping, approvals and sign-off |

Records extend existing session JSON (`impacts`), `PlanItem.flow` and `Task.flowInputs/flowTools`. No new scheduler, model account or memory store. Existing authentication/origin rules apply; new endpoints reject locked workbenches, and snapshots reject stale concurrent writes within one process. This is not a distributed database transaction.

Back up sessions and caches before upgrading/downgrading: older writers may discard new fields. Source verification, actual browser checks, package publication and deployment are separate states; see the [task ledger](../tasks/project-impact-workflows-20261113.json).
