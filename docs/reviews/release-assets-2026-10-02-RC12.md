# aide 0.1.14.0 RC12 Release Asset Report

## Build and verification

- Release tag: `v0.1.14.0-RC12`, source commit: `dce3b63960c969f6be472db235aad5752657862a`.
- Build target: Docker Linux ARM64. Image: `aide:0.1.14.0-RC12`, image ID `sha256:5b6721a2c1e7c1f09b6ef02c1ee76e83c5821c7449718edc6b0f0df0231cf2a2`.
- Full release build ran `go test -mod=vendor -count=1 ./...` and `go vet -mod=vendor ./...`: PASS. `internal/server` completed in 199.834 s; `internal/server/tts` completed in 2.073 s.
- Focused server tests for package upload and stale slot status: PASS. Shell syntax checks ran with the host macOS Bash 3.2.57: PASS. JavaScript syntax checks: PASS.
- Release image health check: PASS. Authenticated `/api/updates/slots` reported `runtimeMode=release-image`.

## A/B upgrade acceptance

- On the RC11 macOS launcher directory, the release RC12 update ZIP was accepted and staged in the inactive slot. Manual activation imported the image and restarted the same Compose project.
- Before the RC11-to-RC12 live acceptance, the RC12 host launcher scripts were overlaid into the existing launcher directory; updating the container image alone cannot replace host-side scripts. The RC12 release notes document this bootstrap step for stock RC10/RC11 macOS installs.
- The live upgrade completed from RC11 slot B to RC12 slot A. `/api/config` reported `0.1.14.0 RC12`, `pending` was null, `lastMessage` was `已切换到槽 A`, and `/healthz` passed.
- The live activation retained the configured host port `8100`. A subsequent bundle restart with `AIDE_PORT=8099` started RC12 on `8099`; this confirmed an existing running bundle is not mistaken for an external port conflict.
- The original failure came from Bash 4-only parameter transformations in the macOS host agent, which runs on Bash 3.2. The agent now uses `tr` and derives the Compose project name from the running container label. It can recover a pending manual activation instead of leaving the UI in “正在切换”.

## Release assets

Directory: `.agent-state/release-assets/v0.1.14.0-RC12/`.

Includes the macOS Apple Silicon, Windows ARM64, and Ubuntu ARM64 launcher ZIPs; Linux ARM64 image archive; Linux ARM64 in-app update ZIP; `SHA256SUMS`; and `RELEASE-ASSETS.txt`. All seven checksum entries passed; all four ZIP archives passed `unzip -t`, and the image passed `gzip -t`.
