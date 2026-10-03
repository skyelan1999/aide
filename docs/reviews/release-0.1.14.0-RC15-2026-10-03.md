# aide v0.1.14.0 RC15

RC15 bundles the assistant harness policy fixes, assistant settings and trajectory fixes, password prompt reveal control, and two independent built-in avatar packs. The enhanced avatar pack is selected by default; the original pack remains separately available.

## Verification

- The repository quick and full verification gates passed before the version bump. The full gate includes the repository's Docker-backed Go race tests and `go vet`.
- `scripts/docker-release.sh` rebuilt the tagged Linux ARM64 image with forced Go tests and `go vet`; the container `/healthz` smoke check passed.
- Release archives were checked against `SHA256SUMS`; the image archive passed `gzip -t`, and each ZIP passed `unzip -t`.
- Real browser UI verification was not completed: Chrome rejected the local TLS certificate (`net::ERR_CERT_AUTHORITY_INVALID`). The certificate warning was not bypassed.
- macOS and Windows launcher execution and an actual A/B activation/rollback were not exercised on this build host. This release is a prerelease for user testing.

## Install and upgrade

- The small platform launcher ZIPs download and verify the matching image on first launch when it is not bundled locally.
- The full runtime ZIP contains launchers plus the Linux ARM64 image for offline first launch, and can also be selected as an in-app update package. If macOS extracts the ZIP automatically, select its extracted folder in Software Update.
- The standalone update ZIP is for the in-app A/B updater. Staging a package does not activate it; activation remains a separate user action.

See [the RC15 asset and verification report](release-assets-2026-10-03-RC15.md) for the image ID, checksums, and evidence.
