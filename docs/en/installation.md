# Installation, updates and first launch

[简体中文](../installation.md) · **English**

[v0.1.19.0-RC2 prerelease](https://github.com/skyelan1999/aide/releases/tag/v0.1.19.0-RC2) was published on 2026-11-12 from `a9f8f412`. Verify architecture, SHA-256 and `RELEASE-VALIDATION.json` in that Release; the [publication receipt](../reviews/2026-11-12-release-preparation/publication-rc2.json) records verification scope. Later main-branch features do not establish support in this or an older package.

## Choose a package

| Package | Target |
| --- | --- |
| `aide-<tag>-full-linux-arm64.zip` | Linux ARM64 Docker engine, including Apple Silicon |
| `aide-<tag>-full-linux-amd64.zip` | Linux AMD64 Docker engine, including common x64 Windows/Linux and Intel Mac installations |
| `aide-<tag>-devtools-linux-<arch>.zip` | Optional development SDK; not needed for daily runtime |
| Git source | Explicit source builds and customization |

Architecture names refer to the Docker engine, not the browser. A full package contains the image and host launch files. Use only assets actually present in one Release. Historical lightweight launchers download their image and cannot substitute for a full A/B update package.

## Host prerequisites

Use Docker Desktop on macOS or Windows (Linux containers, commonly WSL2 on x64 Windows), or Docker Engine with Compose and Buildx on Linux. Source work needs Git. Python 3 supports host scripts and verification; daily runtime does not require host Go, Node or npm. Review Docker Desktop licensing for your organization. Aide is MIT; bundled components retain their own licenses.

Official sources: [Docker Desktop](https://docs.docker.com/desktop/), [Docker Engine](https://docs.docker.com/engine/install/), [Git](https://git-scm.com/downloads), [Python](https://www.python.org/downloads/).

## Full package installation

Download the correct full ZIP and `SHA256SUMS` from the same Release. Verify names and hashes, then extract to an owned installation directory. Use `start.command` on macOS, `aide-launcher.exe` / `start.bat` on Windows, or `start.sh` on Linux. Launchers verify/import the image, check architecture and identity, and wait for service health. Installation does not make a paid model call.

```bash
shasum -a 256 -c SHA256SUMS
```

A complete checksum run needs all files named in that manifest; selective downloads can compare the chosen entry independently. Windows can use PowerShell `Get-FileHash -Algorithm SHA256`. A digest is an integrity check, not an independent publisher signature.

## Source installation and daily commands

```bash
git clone https://github.com/skyelan1999/aide.git
cd aide
# For reproducible release source, check out an actually published exact tag.
bash scripts/install.sh --check
bash scripts/install.sh --source
```

| Operation | Command | Behavior |
| --- | --- | --- |
| Start | `bash scripts/aide.sh start` | Reuse a healthy service or start an installed image; never compile source |
| Apply source changes | `bash scripts/aide.sh build` | Build, verify and start; no silent fallback after build failure |
| Cold build | `bash scripts/aide.sh clean-build` | Apply the scoped cache contract, preserving user data |
| Status / logs | `bash scripts/aide.sh status` / `logs` | Inspect this installation |
| Stop | `bash scripts/aide.sh stop` | Stop owned components, preserve data/configuration |

The corresponding `start.*`, `build.*` and `stop.*` entries have the same responsibilities. Refreshing a page does not replace embedded application code. See the [launcher contract](../architecture/launcher-contract.md).

## Directories and authorization

Source `.env` controls port and Compose/mount configuration; the default HTTPS port is 8097. Release launchers persist their selected port. Do not stop an unrelated service to resolve a port conflict.

```dotenv
AIDE_PORT=8097
AIDE_WORKSPACE=/absolute/path/to/project
AIDE_CONTEXT=/absolute/path/to/reference
AIDE_LOCAL_ROOT=/absolute/path/to/projects
```

Paths must exist; OS permissions and Docker sharing still apply. Switching an application workspace selects an already authorized scope. Settings > Files and Knowledge > Local Directories can add selected directories, followed by native confirmation and apply/restart. The paired current native launcher/helper holds actual directory objects and exposes `/mounts/<id>` through a local file bridge, preventing a replaced pathname from substituting another directory. Changed identities require deliberate removal/reselection. See [workspace paths](workspace-paths.md) and the [bridge contract](../architecture/launcher-contract.md#selected-directory-object-bridge-773), including I/O and concurrent-database limits.

SSH workspaces and enabled AI-visible references retain separate identities; reference writes also require RW. Independent device SSH sessions use their own configuration and credentials.

## Sign-in, models and privacy

First launch creates local HTTPS certificates. Handle a self-signed warning only after confirming the expected localhost service. Touch ID/WebAuthn use `localhost`, not an IP RP ID. Follow the launcher's local sign-in flow; do not share tokens or access-bearing URLs. See [TLS](security/tls.md).

Configure provider Base URL, named key, model and API protocol. Start with nonsensitive input. Installation does not establish model, microphone, browser-control or customer-device acceptance. Cloud providers receive the context, tool results and images actually sent to them.

## Updates and plugins

Back up `.env`, data and configuration and retain the old package/image. Release installations accept a matching-architecture full package in Settings > Software Update using the existing A/B health/rollback flow. Source installations update Git then explicitly build, with release auto-upgrade disabled. See [software updates](../architecture/software-updates.md) and [operations](operations.md).

Public plugins are independent packages. Updating Aide does not replace an installed plugin. SQLite 1.0.3 and StarLink 0.1.6 require market host Aide `0.1.19.0 RC1`; update the host before upgrading those plugins. Remote SQLite requires stopped external writers and a completed WAL checkpoint.

## Offline use and recovery

A full package can be verified/imported and started offline. Daily start uses an existing image without build/pull; do not build on an unprepared offline target. Use a local/intranet model. Online search, marketplace refresh and missing external speech/plugin engines still need connectivity or prior preparation.

Offline source builds separately require base images, build inputs and the correct platform wheelhouse. `scripts/prebuild-wheels.sh --target-platform linux/arm64` (or amd64) plus `PIP_OFFLINE=1` only prepare the Python wheel portion. See [runtime inputs](runtime-bundle.md).

For a missing image, check architecture/tag and build source explicitly if applicable. For a checksum mismatch, stop using the file. For a missing/replaced directory, confirm the drive and deliberately reselect instead of creating an empty substitute. After bridge I/O failure, reread before assuming a save. Missing sessions usually require checking the installation/project/data mounts. Never use `docker compose down -v` or global prune as routine recovery. Keep workspaces, history, plugins, credentials and old images. Release checks and device limits are documented in [Release validation](../testing/release-validation.md).
