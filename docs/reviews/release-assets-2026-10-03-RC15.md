# RC15 release assets and verification report

## Identity

- Release tag: `v0.1.14.0-RC15`
- Source commit: `6a6129a447d06498a7618189ab59413dcb8c11b3`
- Container image: `aide:0.1.14.0-RC15`, `linux/arm64`
- Image ID: `sha256:7b34f4db310cc5ddbd500db3e70ada9c01f46be407d33a1eead05c3c41b65b92`
- Embedded source fingerprint: `e219775f2b81879d371c49053722a9f2e336a42bca83693fac166415275a4552`
- Image archive SHA256: `946506e90f5faf9e122a49e3fd03bcc425cb5cd234b709f5932b09dc895d62cc`

## Verification

- `python3 scripts/agent-route.py verify quick`: PASS.
- `python3 scripts/agent-route.py verify full`: PASS, including Docker-backed Go race tests and `go vet`.
- Focused browser-side checks: avatar settings/migration, assistant mode controls, password prompt, and i18n: PASS.
- `bash scripts/docker-release.sh`: PASS; forced Go tests, `go vet`, image build, and isolated container `/healthz` smoke passed.
- Asset checksum manifest verification: PASS. Image gzip integrity and every ZIP archive integrity: PASS.
- Real browser UI verification: NOT RUN because Chrome rejected the local TLS certificate (`net::ERR_CERT_AUTHORITY_INVALID`). The certificate warning was not bypassed.
- macOS/Windows launcher execution and real A/B activation/rollback: NOT RUN on this Linux ARM64 build host.

## Assets

The asset directory's `SHA256SUMS` is authoritative. Hashes below are copied from that manifest; the manifest itself is intentionally not self-checksummed.

| Asset | SHA256 |
| --- | --- |
| `aide-v0.1.14.0-RC15-linux-arm64-image.tar.gz` | `946506e90f5faf9e122a49e3fd03bcc425cb5cd234b709f5932b09dc895d62cc` |
| `aide-v0.1.14.0-RC15-full-linux-arm64.zip` | `68471bfa08ca3e2cc547c6f963dedcadf1ae5bfa0debb7b79ac10a8b5484a88e` |
| `aide-v0.1.14.0-RC15-update-linux-arm64.zip` | `8ae5608a03ad133130c33c960dcd3f4ece60fcb467d8bf5368f0f7d12600d7e6` |
| `aide-v0.1.14.0-RC15-macos-arm64.zip` | `f513625fbc461bb82a1ecbc24685cdb8737a9c6df9eb583fed323ef94719fa06` |
| `aide-v0.1.14.0-RC15-ubuntu-arm64.zip` | `c3bb69b4b4780787d824975539b481cbbca88370b00a4570bcf9441421095a37` |
| `aide-v0.1.14.0-RC15-windows-arm64.zip` | `efdd180df958750474869c1e9c3940bfe379ad3d096e518d2419ccf8c6e11db1` |
| `RELEASE-ASSETS.txt` | `fd253d8c0830613ccdb5c46013e40c0f038f4d613cd0f7a5979466c5148aa922` |

The macOS/Ubuntu/Windows launcher archives are lightweight online installers. The complete runtime package is about 521 MB and combines all launchers with the ARM64 image; that same ZIP supports offline first launch or in-app staging. The standalone update ZIP contains the image and the update manifest/checksum payload. No x86_64 image bundle was built on this ARM64 host.

## Rollback

RC14 remains the preceding release. For a failed RC15 A/B switch, select the previously active slot after the host launcher reports it healthy. Shared workspace and settings volumes are outside the image slot. Release tags and older assets remain unchanged.
