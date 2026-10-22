# AutoAiModelRouter host delivery · 2026-10-22

Baseline `86d7617c470a0505b9bcf2bdf5c0e044a22f5b98`. Issue [#12](https://github.com/skyelan1999/aide/issues/12) tracks the confirmed missing composer wiring in the inherited pending host integration. The cause of its earlier removal is not established.

The host now validates the declarative protocol 1 offline catalog and exposes Auto at the top of the ordinary conversation/workflow model column only while a valid router plugin is enabled. Selection uses the existing session CAS API and retains a manual fallback. Selecting an ordinary model clears Auto; disabling or uninstalling the plugin hides Auto and routes subsequent requests through the manual fallback. The assistant keeps its independent model source. Request records and parallel parts show model/provider identifiers without keys. Routing is exact-name matching plus explicit aliases and maintainer heuristic weights, not an official cross-vendor ranking.

## Current delivery verification

- Linux ARM64 Go 1.26.10: `go test -mod=vendor -race ./internal/server -run "Test(AutoModelRouter|ConversationParallel|SessionModelSelection)" -count=1 -timeout=6m` PASS, 23.404 seconds. Covers two distinct mock provider HTTP endpoints with overlapping requests and separate credentials, 1X sequential requests, cancellation, capability/window constraints, unknown catalogs and disabled fallback.
- `node scripts/auto-model-router-ui.test.cjs`, `node scripts/session-model-selection.test.cjs`, `node scripts/parallel-chat.test.cjs`: PASS. The new composer regression is registered in the quick profile.
- `python3 scripts/agent-route.py verify quick`: PASS 143 checks, fingerprint `4e115bfdce34984f81c75e83074a8e83728f6280546b92bb1f973a0820025e08`. The first exploratory quick run had no failing individual command but its source fingerprint changed during temporary-fixture cleanup/formatting; it is not used as a passing receipt. The final receipt was collected after those source changes.
- Actual isolated Aide testApp: public AutoAiModelRouter package files imported through the actual host bundle API, then exercised in Chromium against the production embedded page. Auto top entry, session save/reload, ordinary model exit, 5X persistence, disabling/re-enabling/uninstalling and desktop/narrow blue/green light/dark layouts PASS. No horizontal menu overflow, page errors or external browser requests. [Machine result](evidence/auto-model-router-20261022/browser-delivery.json), [isolated viewport screenshot](evidence/auto-model-router-20261022/auto-menu-delivery.png).
- Documentation links and diff whitespace checks PASS.

The earlier [candidate evidence](evidence/auto-model-router-20261022/acceptance.json) is historical and is explicitly not the final delivery fingerprint. Its Safari screenshot is kept locally rather than republished with user browser chrome.

## Boundaries

No vendor-paid model requests, native Anthropic/Gemini protocol validation, Windows/AMD64 host execution or new Aide full release gate was performed for this source-only delivery. The coordinator will integrate this source with the current mounts/market changes before the full gate. The public Aide RC3 image predates protocol 1; an AutoAiModelRouter plugin release must clearly require the matching source host until a new host package ships. No production container was replaced, and no commercial plugin sources, customer data, credentials or private packages are included.
