# aide v0.1.14.0 RC9 Release Asset Report

## Build and runtime verification

- Release build: PASS on macOS Apple Silicon, `linux/arm64` image.
- Docker build ran `go test -mod=vendor -count=1 ./...`, `go vet -mod=vendor ./...`, and a trimmed production build: PASS. The server package suite completed in 197.6 seconds.
- Container `/healthz` smoke check: PASS; temporary container was stopped and removed by the release script.
- Image ID: `sha256:dd7c13761d541f6854c16f0f7f6ceeb6ce2476433015dd41a0687363d4e8855a`.

## Assets

Directory: `.agent-state/release-assets/v0.1.14.0-RC9/`.

| Asset | Size | SHA256 |
| --- | ---: | --- |
| `aide-v0.1.14.0-RC9-linux-arm64-image.tar.gz` | 490 MiB | `cd33208031dd6c99f9398b2770c22b3c6c3f725e0776a2401a468d535b2ffd82` |
| `aide-v0.1.14.0-RC9-macos-arm64.zip` | 11 KiB | `eff1f7d02da39ed7a578aae80e9ef79d6524a6949b68d2226eee094ad91dced0` |
| `aide-v0.1.14.0-RC9-ubuntu-arm64.zip` | 11 KiB | `d8543f8f4607218a1e605feef2185c4e1a16f9f25fbc00ccff92a8dbe3060abd` |
| `aide-v0.1.14.0-RC9-windows-arm64.zip` | 17 KiB | `d9ed04feef09329157e3c8f8e5233541b3af9aa30c64f8fa2964454adcde8a3b` |

The RC9 macOS package README documents Gatekeeper approval and the narrowly scoped `xattr` fallback. The package remains unsigned and unnotarized; no valid Developer ID signing identities were available on the build host. macOS Finder first-launch acceptance, Windows ARM64, and Ubuntu ARM64 runtime acceptance: `NOT_RUN`.

## Remote publication

- GitHub prerelease: https://github.com/skyelan1999/aide/releases/tag/v0.1.14.0-RC9
- All six assets report `uploaded`; the remote `SHA256SUMS` file matches the local file byte for byte.
- Remote `SHA256SUMS` SHA256: `30229b39a56c6ebf9e8391b59e140ffa54189901fff5dedd337a6ac1fe14b4c9`.
- `main` and annotated tag `v0.1.14.0-RC9` were pushed at source commit `046f100`.
- Failed historical candidate tags RC3, RC4, and RC5 were inadvertently included by `git push --follow-tags` and immediately deleted from the remote; `git ls-remote` confirmed they are absent.
