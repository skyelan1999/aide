# aide v0.1.14.0 RC10 Release Asset Report

## Build and runtime

- Source commit: `ca98af38a24c72755d09323729bc9d30f6e6fb76` (`v0.1.14.0-RC10`).
- Build platform: Docker Linux ARM64.
- Full release build: PASS; `go test -mod=vendor -count=1 ./...` passed, including `internal/server` in 199.211 s; `go vet -mod=vendor ./...` passed.
- Container `/healthz`: PASS.
- Image: `aide:0.1.14.0-RC10`, image ID `sha256:66232bea84299e1ffd083dde2e6cea1d727f720e452ff56b234a5426309f2fce`.
- Runtime mode: `release-image`.

## Assets

Directory: `.agent-state/release-assets/v0.1.14.0-RC10/`.

- `aide-v0.1.14.0-RC10-linux-arm64-image.tar.gz`
- `aide-v0.1.14.0-RC10-macos-arm64.zip`
- `aide-v0.1.14.0-RC10-ubuntu-arm64.zip`
- `aide-v0.1.14.0-RC10-windows-arm64.zip`
- `aide-v0.1.14.0-RC10-update-linux-arm64.zip`
- `SHA256SUMS` and `RELEASE-ASSETS.txt`

The application update ZIP passes `unzip -t` and contains exactly `manifest.json`, `SHA256SUMS`, and one matching Linux ARM64 image archive. The macOS launcher ZIP passes `unzip -t` and contains executable `start.command`. The launcher downloads the matching image from the public Release when it is not already available locally.

## Scope

RC9 does not contain the updater interface. RC10 is the installable bridge: starting its launcher replaces the earlier service under the same Compose project while reusing the named data volumes. Manual A/B update testing starts after RC10 is running. RC11 is built separately as the next-version test target.
