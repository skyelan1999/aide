# Prebuilt runtime delivery

Ordinary source and release launchers now use complete prebuilt runtimes. A source checkout chooses one architecture-specific record, validates archive SHA-256 before import, then verifies exact image ID, platform, source identity and CAD runtime. Failure stops before activation/data migration; no implicit source compilation or base-image download follows. Explicit developer builds remain available.

The legacy container owner selector now uses the intended Compose project/service, ignores one-off tasks, and refuses genuine ambiguity before stopping or importing anything. POSIX and Windows selectors follow the same rule; native Windows acceptance is user-owned.

## Verified local artifacts

Both ARM64 and AMD64 complete archives were exported, independently rehashed, inner image tar checksummed, and actually imported into Docker. Exact launchers, licenses, platform/source/CAD metadata and absence of private data were checked. Source identity is `e5baab8f73a3d970c1e493b7a86e74982eee6b743c5951eed51ce7b85248d85e`; compiled product code is commit `373fd4d6`.

ARM64 image `ab8df956177b` runs the workbench and CAD service on 9999. Actual `start.command` used the prebuilt manifest without compiling. A fresh extracted package also started through `start.command`, created isolated local data, served authenticated native CAD with all 120 expected fixture triangles, and shut down through `stop.command`. Credentials remained private, CAD mounted only its read-only derived credential, and production image/data remained unchanged. Prior global slot image tag was restored.

AMD64 image `ec1b03234c08` completed the same native geometry regression under local CPU emulation. This verifies Linux AMD64 binary/runtime behavior on this machine, not Windows native launching or receiving-device import. Launcher unit tests cover architecture mismatch, identity/checksum/download failures and no compilation fallback. Actual Docker owner/migration/rollback tests preserve all eight protected fixture files and permissions and start an ordinary checkout without Dockerfile/Go/Git.

Machine-readable local artifact and fresh-start evidence is [recorded here](evidence/prebuilt-runtime-packages-proof.json). The private real STEP model, geometry and screenshots are excluded from all packages and public evidence.

## Publication status

Local build, package verification/import and fresh ARM64 startup: PASS. Final exact-input full check: PASS (105 checks, Go race/vet and CAD geometry included), fingerprint `240b63b1b94628a989e2046cb4c8e1c91df162af08b7f492ef0e2e94305e91ac`; all three scoped main release gates: PASS. ARM64 complete attachment: uploaded and public API verified at 770,934,573 bytes, SHA-256 `3009d01b7ae14d099906f060d6e6d6260782bcd7e1df6dae68154685e9dcfa55`; original uploaded assets preserved. AMD64 attachment is uploading. Remote main push, AMD64 public verification and final instructions/checksum attachments remain pending. See [ARM64 remote proof](evidence/prebuilt-remote-arm-proof.json). Existing RC2 assets and tag remain intact; original local runtime and offline archives remain available for rollback.

Do not interpret local image/package existence as receiving-device success. Windows native testing belongs to the user; real vector testing remains deferred. The original continuity/history/authorization/outcome/knowledge objective remains open beyond this delivery batch.

A real negative launch check extracted the complete AMD64 package on the ARM64 engine and invoked its actual `start.command`: it refused the mismatching architecture before data migration, created no service container, and preserved the production container/image. This is [platform-refusal evidence](evidence/prebuilt-platform-refusal-proof.json), not native Windows acceptance.
