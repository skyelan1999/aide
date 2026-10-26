# Startup and official release-image fallback

[简体中文](../runtime-bundle.md) · [English documentation](README.md)

Docker images and monolithic Git LFS bundles are not stored in ordinary Git history for new delivery. Clone/pull transfers source and tracked manifests. Official offline packages carry their validated runtime archives separately.

Startup first verifies a configured local image's platform, image ID, source identity and relevant runtime integrity. A valid image can be reused and health-checked without rebuilding. It does not choose an image from arbitrary tar names or file timestamps.

If no complete local image is available, startup tries the documented local source/dependency path. If preparation or compilation fails, `docker/prebuilt-images.tsv` selects the unique official release matching the architecture. A verified cached archive is reused or downloaded when absent. SHA-256, imported image ID, platform, source identity and applicable runtime components are checked before activation. Failure stops startup rather than treating an incomplete package as successful. A fallback release can be older than the checkout; the running image determines the actual version.

Use `start.command` on macOS, `start.bat` on Windows and `bash start.sh` on Linux. A complete offline bundle imports the `.aide-image` runtime without source compilation. A Git pull does not force replacement of a valid running image.

```sh
AIDE_FORCE_REBUILD=1 bash start.command
# Require source build to fail instead of falling back to an official image:
AIDE_RELEASE_FALLBACK=0 AIDE_FORCE_REBUILD=1 bash start.command
```

PowerShell:

```powershell
$env:AIDE_FORCE_REBUILD = '1'
.\start.ps1
```

`AIDE_BUILD_FROM_SOURCE=1` bypasses the normal local-image reuse path. `AIDE_BUILD_FROM_SOURCE=0` selects image mode. Preserve configuration, workspaces and data independently of image archives. Validating a download is separate from importing, running and accepting it on a target device.

For old LFS-enabled checkouts, `git lfs uninstall --local` removes the local LFS filter/hook when appropriate; do not remove unrelated hooks. Existing remote history is not rewritten. Keep usable recovery images and architecture-specific release archives; an upgrade does not require clearing Docker.
