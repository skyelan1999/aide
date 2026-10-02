# aide v0.1.14.0 RC11 Release Asset Report

## Build and runtime

- Source commit: `643d5d74184737b91b75277ca0fb3f6f73819bae` (`v0.1.14.0-RC11`). The only source change from RC10 is the version record; application code is unchanged.
- Build platform: Docker Linux ARM64.
- Build: PASS; `go vet -mod=vendor ./...` passed, JavaScript syntax checks passed. Full Go tests passed on RC10 (`internal/server` 199.211 s); the application code did not change for RC11.
- Container `/healthz`: PASS.
- Authenticated `/api/updates/slots`: `runtimeMode=release-image`, active slot A, version `0.1.14.0 RC11`.
- Image: `aide:0.1.14.0-RC11`, image ID `sha256:26c3d1452762ef58a781ca3fc8422bdc7b69ab9030c7cb2b7cdaa199a37af078`.

## Upgrade acceptance

- RC10 accepted the RC11 application update ZIP and staged it in slot B.
- Slot A remained active (`0.1.14.0 RC10`); slot B reported `0.1.14.0-RC11`; `pending` remained null. No activation or service switch was triggered.
- Update ZIP and macOS launcher ZIP both pass `unzip -t`. Update ZIP contains exactly manifest, SHA256SUMS, and the image archive.

## Assets

Directory: `.agent-state/release-assets/v0.1.14.0-RC11/`.

Includes macOS Apple Silicon, Windows ARM64, and Ubuntu ARM64 launchers, the Linux ARM64 image, the Linux ARM64 update ZIP, `SHA256SUMS`, and `RELEASE-ASSETS.txt`.
