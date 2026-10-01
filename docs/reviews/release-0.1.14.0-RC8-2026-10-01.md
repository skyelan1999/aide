# aide 0.1.14.0 RC8 Release notes

RC8 fixes the launcher package handoff identified in RC7. Platform ZIPs no longer require users to manually download and copy the Docker image before starting.

## What's included

- macOS Apple Silicon, Windows ARM64, and Ubuntu ARM64 launcher ZIPs.
- A shared linux/arm64 application image and `SHA256SUMS` attached to the same GitHub Release.
- On first launch, each platform launcher downloads the matching image and checksum manifest when the image is not already available locally. It verifies the archive SHA256 and imported Docker image ID before starting the application.
- The launchers never build the application or pull base images. Offline installation remains available by placing the image archive and `SHA256SUMS` in the extracted package's `docker-images/` directory.

The first online launch requires internet access and downloads approximately 500 MB. Docker Desktop or Docker Engine with Compose v2 is required. Windows x64 and Ubuntu x64 image packages are not included.

## Verification status

The final release build passed the full Go test suite, `go vet`, and compilation. A temporary container health check passed. The offline launcher suite passed 5/5 cases. The image archive passed gzip integrity; all three launcher ZIPs passed ZIP integrity; all six local SHA256 entries passed. GitHub reports all six assets uploaded, and the checksum manifest downloaded from GitHub matches the local manifest byte-for-byte.

Published prerelease: <https://github.com/skyelan1999/aide/releases/tag/v0.1.14.0-RC8>. Tag `v0.1.14.0-RC8` points to `43dd46ccb926b46c17f40820a993200f313657b1`; remote `main` is `ed6605289f3b6bd4fcac5990a6da31606a2cf9e6`. The linux/arm64 image ID is `sha256:7e4bcf76debbb3c85fae94b4540b23487f8eeb1f9822ebb1728988b6c575de43`; the remote image archive is 513,819,739 bytes. Its remote checksum is `2904246b05da2f2976d5588234a62a0c124b6eae9a46f53ecc86b83cb7ca864e`.

The startup scripts were not run on Windows or Ubuntu host machines. The new GitHub-download path was reviewed and packaged, but a first-start download through a fresh user's Docker installation was not live-tested. See the [release ledger](../tasks/release-2026-10-01-rc8.json) for exact evidence and limits.

## Rollback

Use the previous RC7 platform launcher and matching RC7 image assets. Do not remove or overwrite Docker volumes when rolling back.
