# Runtime packages, explicit builds and recovery

[简体中文](../runtime-bundle.md) · [Installation](installation.md) · [Launcher contract](../architecture/launcher-contract.md). Reviewed from source `05e0b8a7` on 2026-11-12.

## Start and source updates

Use start.command on macOS, start.bat on Windows or bash start.sh on Linux. Start reuses a healthy instance or starts the installed image. It does not compile source or replace a failed build with another Release. A source checkout without an installed image stops and asks for an explicit build.

```sh
bash scripts/aide.sh build  # first build or apply changed source
bash scripts/aide.sh start  # daily startup
```

Windows uses build.bat / start.bat. Initial source bootstrap needs Docker Engine, Buildx and system PowerShell to build the native launcher inside Docker. A successful build verifies and starts the new artifact; failure preserves the error instead of silently falling back to an older program. Check source, image and running binary identities separately.

Old AIDE_FORCE_REBUILD=1 start, AIDE_RELEASE_FALLBACK and start.ps1 instructions are not the current source-update interface. Browser refresh does not replace embedded application code. Use the scoped [maintenance entries](docker-maintenance.md) and preserve data/configuration.

## Full packages and optional SDK

Git clone/pull transfers source and tracked manifests, without Docker images or a monolithic LFS archive. Download the full Release ZIP matching the target Docker engine (linux/arm64 or linux/amd64), verify the published SHA256SUMS, then extract and use its included start entry. First startup validates the image marker, archive hash, platform and imported identity. Later starts reuse the installed image without compiling source.

The development SDK is a separate optional artifact, not the normal runtime package. It does not import an installation's data or plugins. Public plugins are released independently; respect their marketplace minimum host version when upgrading them after Aide.

Release installations support [A/B updates](../architecture/software-updates.md): verify a matching-platform package, stage an inactive slot, activate explicitly and check health, with the existing failure recovery. Source installations update through explicit build. Image rollback does not restore migrated data or user files; keep backups and older artifacts.

## Dependencies and evidence

Running a downloaded full package offline differs from building source offline. Source builds separately need platform base images, wheelhouse, pinned sources and tools. A dependency image is not proof that the new application binary passed. STEP/CAD engines are excluded from the default distribution; plugin engines require their own preparation.

The controlled prebuilt-image manifest records only verified public artifacts. Candidate metadata, public asset digests, archive CRC/SHA, import identity and runtime health are separate checks. The matching tag's RELEASE-VALIDATION.json identifies actual release evidence. Publication does not deploy the maintainer's or customers' installation, or establish physical-device permissions.

For an older LFS-enabled checkout, git lfs uninstall --local removes the old local filter/hook when appropriate; retain unrelated custom hooks. Remote history is not rewritten. Keep usable recovery images and architecture-specific packages rather than clearing Docker for an upgrade.
