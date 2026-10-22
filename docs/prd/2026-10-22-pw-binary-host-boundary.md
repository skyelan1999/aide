# ProfessionalWorkflow binary host boundary

Aide supplies the manual session, approval, model and scoped tool bridges. The separately installed ProfessionalWorkflow runtime owns workflow graph validation, expansion and scheduling; that implementation is not bundled into the public host source or image.

## Trust and execution

The reserved capability requires the enabled `professional-workflow` plugin, `index.js`, version `0.4.0`, and `workbench.html`. A fixed Ed25519 public key verifies the exact bytes of `runtime.json` against the base64 signature in `runtime.sig`. Schema version and protocol are 1. The signed descriptor binds the exact length and SHA-256 of `manifest.json`, `index.js`, `workbench.html`, and the selected `bin/linux-arm64/pw-engine` or `bin/linux-amd64/pw-engine`. No environment setting disables verification. The exact HTML bytes served by the workbench are checked against the signed digest in addition to the normal ticket checks.

The Linux host copies verified executable bytes into a sealed memfd and executes that descriptor, eliminating a subsequent executable path lookup. Each request has an eight MiB input limit, a 256 KiB output limit and a ten-second deadline. The child receives a fixed minimal environment and no model credentials. Missing, modified, unsigned or unsupported packages keep PW mode unavailable and return an actionable host error when invoked directly.

## Runtime protocol

Requests contain `protocol`, `op`, `definition`, `input`, `values`, `completed` and `active`. `validate` checks a definition. `advance` selects one next step; the host calls it serially with a fresh execution snapshot. `active` records nodes whose branch was selected, rather than background work. Replies contain `protocol`, `ok`, optional `error`, `done`, and `step`. A step contains its original node ID/type, skip/pure flags, expanded text/arguments and a string output. The runtime has no Aide AI tool registration or direct access to host bridge objects.

Aide independently checks field limits, immutable node identity/type, model settings, file bindings, plugin state, workspace identity/revision and the existing dispatcher permissions. Native runtime output cannot replace a node's tool, bindings or reasoning policy. External actions continue through original approval and proposal handling; execution does not convert a write proposal into a saved file. Stale workspace approval/cancellation is rejected. Disabling tools continues to use the shared dispatcher, including automatic bound-file inputs.

## Verification boundaries

Native integration tests may mount an explicitly supplied signed package via `PW_TEST_RUNTIME_DIR`. This test setting selects fixture files only; it does not change the production key or validation policy. Without that separate artifact, runtime-dependent integration cases explicitly skip, while missing/invalid-package and generic host permission tests remain available. Mock provider tests do not demonstrate live paid-model operation. Browser, AMD64 emulation, full-host regression and production deployment are separate acceptance steps.
