# Optional plugin workbench port · source candidate

## Change and ownership

Continuous RfToolBox work needs a controlled path for future knowledge/report operations. This generic host extension is also compatible with the BrdPlan bridge contract inspected as an interface reference. No authorization is inferred from plugin-generated text. Changes remain in this Aide host checkout; unrelated manifests/registry and upgrade backups are untouched and unstaged.

Old protocol1 plugins with no workbench.tools stay offline. Opted-in tools must belong to the same enabled plugin and pass the existing pluginPlanPolicy, disabled/sandbox policy and resource admission. Builtins, cross-plugin tools, browser/computer-control tools and top-level source aliases are rejected. This does not yet expose document_search or reference retrieval, so RF controlled knowledge integration remains incomplete.

## API/client contract

- workbench.tools: optional, up to16 valid unique tool names.
- open returns bridge.protocol=1, instanceId, tools to the parent only; HTML ticket remains single use. Instance lifetime30min, <=8 active; page load activates it.
- POST /api/plugins/{id}/workbench/{instance}/call: normal bearer/origin middleware; protocol/requestId/method/tool/arguments/attachments or proposalId.
- tool.call: one active call, <=128 tool IDs without replay; result/proposal_pending/error. Normal resource scheduler, queued admission recheck and per-instance cancellation. No automatic retry.
- proposal.status: only proposals owned by this instance; status/applied observations, never apply authorization.
- DELETE instance cancels the active call and revokes it.
- Parent initiates the MessagePort only after a ready message whose source is the active iframe.contentWindow. Opaque-origin wildcard is limited to the fixed destination window. No parent bearer or instance reaches frame.
- Reference client: internal/server/web/plugin-workbench-client.js, bundled inline by plugin authors. create(pluginId), connect(), call(), proposalStatus(), onEvent(), close(); no approve method.
- A pending file proposal is persisted in a normal host session with AutoReview=false. Parent review button closes workbench and selects this session. Existing-file versions must be attached before dispatch. Approval, target binding and content conflict checks remain in existing apply handlers.
- Parent observes most recent proposal every~1.5sec and emits proposal.changed. Native selection/dirty events and desktop stream are not implemented.

## Verification actually run

1. Existing workbench tests passed on first compile.
2. First bridge regression found a fixture's write tool incorrectly declared resources:[]; existing pluginResourceResult refused its proposal. Fixed fixture write declaration and kept a dedicated negative regression; production protection unchanged.
3. `GOMAXPROCS=2 go test -mod=vendor -race -v -count=1 -timeout=5m ./internal/server -run "TestPluginWorkbench"`: PASS,43.279s, in one isolated dependencies container limited2CPU/3GiB. Covers ownership/replay/disabled policy, manual proposal before/after host approval, stale scope/page/package/close, dispatched cancellation, sandbox and read-only contract, existing auth/CSP/ticket/path behavior. Raw log ignored .agent-state/workbench-bridge-race.log.
4. `node --test scripts/plugin-workbench.test.cjs`:5 pass. Parent source matching, sandbox, lock/scope lifecycle, declared tool methods/replay and late results.
5. `node --test scripts/plugin-workbench-client.test.cjs`:2 pass. Only parent/plugin handshake; no automatic retry or approval; pending result and revocation handling.
6. Syntax checks app.js, plugin-workbench.js/client.js and locales/en.js pass. No full Aide test suite, browser, native CAD or RF instrument result is claimed.

## Sequential coordinator/security review

Per docs/agent/WORKFLOW.md, stage roles were performed sequentially by the current agent; this is a source review, not an independent third-party audit. Traced normal executeToolCall, callPluginToolContext, pluginPlanPolicy/resources, recordToolProposal and manual apply handlers before adding the adapter.

Reviewed invariants: parent-only token; explicit own-tool allowlist; server scope/generation/enable/package/page checks; one-use page ticket; request replay/busy limits; no approve/file/network method; pre-call attachment baseline; post-call scope check before proposal persistence; no output accepted after client revocation. SDK, parent and server all preserve proposal_pending versus applied semantics. No global CSP relaxation.

Boundaries: Node plugins are already trusted executable code, not sandboxed by a resource declaration or this port. Cancellation at~200ms/300ms is best effort and cannot undo an external side effect; plugin operations need their own durable/idempotent receipts. Package digest follows the installed registry package identity; out-of-band editing of trusted plugin code is not an OS containment boundary. Source/reference visibility checks are reused for proposal targets and snapshots; arbitrary builtin retrieval is deliberately absent.

## Remaining / deployment

Local deployment completed once via AIDE_OPEN_BROWSER=0 AIDE_BUILD_FROM_SOURCE=1 bash start.command. No push or release/tag. Saved browser policy still blocks the approved loopback origin; no alternate surface is used to bypass it. Real opaque iframe handshake, review button/navigation, native plugin prepare→host approve→preview→confirm→readback remain NOT_RUN. The RF continuous goal is still active.


## Local deployment evidence (2026-10-18)

- Running buildCommit baac9114a26e9638488934e06bee0b21e7115dfb-dirty, image sha256:320fd8c4f2e5ff86c420713da1435a269b015fa12e22e6c665c023971882e523. Dirty state consists of preserved runtime plugin registry/manifests and installed packages.
- Existing startup resource guards, offline dependency build, JavaScript syntax, go vet and compile completed; no parallel heavy build or full regression.
- Health status ok; integrity remains degraded because persistent first-volume binary baseline at commit341458d6 differs from deployed binary. Baseline preserved, not overwritten to hide warning. This is not a clean integrity acceptance.
- BrdPlan0.4.2 installed through authenticated /api/plugins/bundle; RfToolBox0.1.0 retained. Both enabled, error empty. Bundle SHA256 BrdPlan c0697f4616e733f83b9afce421e4f1f5b2c17e774e56779f390bca6e6476fdeb; RF 2c778cb73ffc22953d501dce7cd44528f36ceddd935f90740714cbfa827251cb.
- All30 BrdPlan and3 RF non-manifest files match package bytes. Installed manifests differ only in omitted empty settings and added installedAt; identity/version/workbench declarations match.
- Old image retained sha256:2b00868423e6d60c0be1ef927db3a48807bc19fe0e583d6aa1f859a8e23f3b33. Rollback uses this ID with AIDE_IMAGE and scripts/aide.sh start-image without volume deletion. Raw evidence under ignored .agent-state/workbench-deployment/.
- No browser origin retry, iframe handshake, native write or approval issued. Real UI/native acceptance remains NOT_RUN.
