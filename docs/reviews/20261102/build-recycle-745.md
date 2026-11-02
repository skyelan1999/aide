# Build recycling and disk recovery — issue 745

Source acceptance head: `2d3da8d0f3dc45851c979e7fd7c7bc16263d5158`.

The user's build.command was blocked before compilation: 20,723,472 KiB free was below its existing 20 GiB host-space requirement. The prior experience.js browser initialization error was independent of compilation. The disk threshold was retained.

## Implemented behavior

The POSIX build coordinator now rechecks the new runtime's installation/image identity, waits up to 30 seconds for Docker's starting health marker, and requires a successful application health response before recycling. It inventories all images including untagged intermediates, removes only obsolete scoped slim runtime outputs without forced image deletion, checks container/tag references again, and retains configured dependency images, build/test toolboxes, seeds and foreign references.

Only the owned dedicated builder gets regular-layer GC (unused for 24h, 20GB cache target). Compiler/download cache mounts and recently used layers remain; the target is not a hard total-cache ceiling. Default/shared builder cache is not pruned. Low-space preflight attempts the same scoped recovery and repeats the original disk check. Compilation/start failure never enters post-build GC; cleanup failures are reported separately without misreporting a healthy build as failed.

Automatic recycling does not remove containers or sweep data volumes. The user's additional explicit image/volume cleanup request authorized a separately inventoried one-time legacy cleanup described below. Native Windows and isolated selfdev A/B builds were not changed.

## Actual verification

- 33 launcher contract tests PASS: active/foreign/exited references, dependencies, multiple tags and untagged objects, reference races, low-space retry, failed build/start, health delay/timeout, cleanup errors, dedicated/default builder boundaries.
- Final quick PASS at source head above, fingerprint `7ac1173e96b2f99bec6aa3783c00cc87e0f218dfb9936391c63181b895d4d09b`.
- Documentation/diff checks PASS.
- Ran the user's actual primary-checkout `AIDE_OPEN_BROWSER=0 BUILDKIT_PROGRESS=plain bash build.command`, without interception or a custom build-only launcher. Final run exit 0: source fingerprint, Docker compilation/vet, image validation, installation startup, healthy wait and automatic recycling all completed.
- First integration run exposed a starting health marker after successful application startup; fixed with bounded health wait. The next run exposed default image listing omitting untagged intermediates; fixed with `image ls -a` and matching regression. Neither partial run was used as final complete recycling acceptance.
- Accepted final build removed 16 obsolete images automatically and reported zero additional old-layer bytes; preceding confirmed GC removed 7 obsolete images and reported 9.763 GB. Initial recovery of reviewed old private immutable regular layers had reclaimed 8.761 GB.
- Canonical `python3 scripts/launcher.py verify`: Docker/Compose/configuration/Git/Buildx/runtime all PASS.

Current image: `sha256:4bce562e08fc1aafd021c162f6cfcd42562a9e8730608b79ceae46ab4f3005d9`.
Source label: `38c7485d29b5b3591c330676e19aa8915d3258c1e999335c70dfbe8db688beb2`.
Runtime health: healthy. Data/workspace/context/home mount identities equal the initial container's mounts.

## Inventory and explicit cleanup

Initial inventory: 32 images and 24 volumes. Final inventory: 7 images, 12 volumes, zero untagged images. Removed 26 images from the initial inventory plus 2 superseded outputs from integration runs. Explicit one-off QA image removals were codex-host-cache-743-test, browser-balance-667-qa and terminal-x11-qa-20261102 after verifying no container references. Compiler bases, BuildKit, the SDK seed and required CI/tool images remain.

The two exited review containers and one never-run /bin/true extraction container retaining the last two untagged images were inspected and removed without -v or forced deletion. Their bind-mounted host evidence was preserved. Other stopped containers were retained.

Twelve unreferenced legacy cache volumes were inspected read-only and verified to contain only a standard Go build-cache layout or to be empty before removal:

- `aide-assistant-loop-go-cache`
- `aide-auto-router-go-cache`
- `aide-brdplan-go-cache`
- `aide-browser-balance-667-go-cache`
- `aide-codex-host-go-cache`
- `aide-go-build-cache`
- `aide-go-modules`
- `aide-go-test-cache`
- `aide-plugin-go-cache`
- `aide-startup-go-cache`
- `pw-native-go-cache-20261022`
- `rftoolbox-native-gocache`

Their combined filesystem cache size was about 26 GiB. This does not equate Docker's logical volume bytes to instantly returned macOS disk bytes. The current BuildKit compiler cache, referenced multi-mount cache, native toolchain, TestBench credentials/runs, preview/plugin data and home/secret volumes remain. Final available host space was about 70.0 GiB and can fluctuate with APFS and system activity.

Receipts, before/after inventories, exact removal lists and actual command logs remain in ignored installation `.agent-state/disk-recovery-20261102/`; regression/quick logs remain in the task worktree `.agent-state/build-recycle/`. No global prune, Docker Desktop resource/proxy changes, workspace data deletion, remote push or public release occurred.
