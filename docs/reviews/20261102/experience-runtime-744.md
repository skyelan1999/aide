# Experience runtime initialization — issue 744

Source fix: `e70300c326e0fb3272c9d4f5e91b97ae4828aa4a`.

Removing the startup replay control also removed creation of `window.aideExperience` from the intro module. The following navigation module still assigned methods onto that absent object, throwing during every full-script startup. It now owns API initialization and retains any existing API identity. Asset cache version advances to 5. Startup and replay controls stay removed, while default intro and reduced-motion behavior remain intact.

## QA inventory and actual results

- Complete script startup, normal/hidden/reduced state, missing/null API, immediate leave/panel callbacks, retained API identity: `node scripts/experience-runtime.test.cjs` PASS. The original script failed this test at its Object.assign export.
- Existing settings browser test now executes the complete experience script instead of only the intro fragment. Desktop/narrow settings autosave, rollback/order/remount and intro checks PASS.
- Final quick profile PASS, head above, fingerprint `45c464f1f14a11e11d7bce9d2b2cd8f3430fb5d619259db98e34b7772a880671`. Documentation and diff checks PASS.
- WebKit reproduced the user's original exception against the old live asset. An owned browser override first verified the fix, then the built candidate and updated production were tested without overrides.
- WebKit complete-script fixture: normal intro count 1; reduced intro count 0; API ready and panel update immediate, zero page errors.
- Updated live 9999, fresh isolated browser sessions: desktop 1440x1000 and narrow 390x844/reduced-motion both had zero page errors and successfully opened settings. Desktop file panel close/reopen passed. Narrow sheet bounds x=10,y=62,width=370,height=720 within viewport; document scroll width=390. No production settings writes or AI calls were made.

The first extended browser check timed out; it was not counted as acceptance. A narrow follow-up initially looked for the desktop settings entry before opening the mobile sidebar, and removing that temporary server caused a request error in its old browser context. Final fresh sessions against stable 9999 completed the actual sidebar/settings path with zero errors. These harness issues were not attributed to the fixed runtime defect.

## Build and local runtime

Canonical launcher build-only passed vet, compile, image identity and source checks without changing the running service. Isolated candidate health passed and its served experience.js bytes equaled the source; index referenced v5. Temporary candidate containers were removed.

Image: `aide:experience-runtime-744-e70300c`.
ID: `sha256:b11fda49b7d3743835c0aefbfa55422c95696787c91ffe99c91ec7f42309c4c2`.
Source label: `66cc3461cd337bffbb48aac30bab770fa224887412765857fc42ed34c5cd339b`.
Served experience.js SHA-256: `b1caed705d07e49f4fc2a09f0a616e4f50a98bf14839aa2d04cda03491d8c8ff`.

Under the user's prior update-9999 authorization in this repair session, configure_image and start-built then switched the existing installation to this image. Health is healthy, healthz integrity/status ok, live v5 asset and WebKit checks passed. Existing data/configuration/key mounts preserved. Previous runtime image `sha256:00c254cb2230269b2654dacee04882d135da8de9e0ea6afe956fc9ab3fb7a7aa` and private .env backup are retained for rollback. No remote push or public release occurred. User's Safari tab is not refreshed automatically; it must reload to replace its already loaded old script.

Evidence retained in ignored task `.agent-state/experience-runtime/` and installation `.agent-state/experience-runtime-744/`. Browser screenshots containing existing workspace metadata are not published. Owned browser processes, contexts, servers and temporary containers were cleaned up.
