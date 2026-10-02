# RC14 release asset report

## Identity

- Release: [v0.1.14.0-RC14](https://github.com/skyelan1999/aide/releases/tag/v0.1.14.0-RC14)
- Tag target: `6a8d3d0e3cb3fa4a0e1993edf62a4f8ff4651ef0`
- Container image: `aide:0.1.14.0-RC14`, `linux/arm64`, image ID `sha256:affcf2ddf6e72e1b6513c5f0813ececee2cd009b4226ac801264877cd26c21fd`
- Image source fingerprint: `054103f220845d54d3d2f53dc211cc02381e5b0e15c1170c0decbe6a98153e91`
- Image archive SHA256: `a08040fd5ff3abb129162e496ad180acb37ba86d768d6a7d7aecc73ead478157`

## Verification

- `python3 -m unittest scripts/test_offline_bundle.py -v`: six tests passed, including simulated partial transfer followed by resume, checksum validation, image import, and browser-open invocation.
- `python3 scripts/agent-route.py verify quick`: passed.
- `python3 scripts/agent-route.py verify full`: passed, including `go test -race -count=1 ./...` and `go vet ./...` in Docker.
- `bash scripts/docker-release.sh`: release build passed its forced Go tests, `go vet`, and container `/healthz` smoke check.
- Published `aide-v0.1.14.0-RC14-macos-arm64.zip`: real first-run download of the 490 MB image, SHA256 verification, Docker import, isolated service start on port 18297, and `/healthz` all passed. The OS browser-open command completed successfully. The Mac was locked, so Safari's visible page was not inspected.
- All seven checksummed release assets were compared with GitHub's remote SHA256 digests; all matched. The attached `SHA256SUMS` is the manifest and is not self-checksummed.

## Assets

The GitHub release contains the macOS, Ubuntu, and Windows ARM64 launcher ZIPs, the full runtime ZIP, the A/B update ZIP, the Linux ARM64 image archive, `SHA256SUMS`, and `RELEASE-ASSETS.txt`.

The lightweight macOS ZIP is approximately 17 KB and downloads the image on first launch. The full runtime ZIP includes the image for offline installation and also supports extracted-folder A/B updates.

## Rollback

RC13 remains published and unchanged. If RC14 causes a problem, use the RC13 full runtime package or switch back to the previously installed A/B slot. User workspace and settings data are stored outside the image slots.
