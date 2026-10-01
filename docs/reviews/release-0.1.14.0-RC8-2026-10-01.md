# aide 0.1.14.0 RC8 Release notes

RC8 fixes the launcher package handoff identified in RC7. Platform ZIPs no longer require users to manually download and copy the Docker image before starting.

## What's included

- macOS Apple Silicon, Windows ARM64, and Ubuntu ARM64 launcher ZIPs.
- A shared linux/arm64 application image and `SHA256SUMS` attached to the same GitHub Release.
- On first launch, each platform launcher downloads the matching image and checksum manifest when the image is not already available locally. It verifies the archive SHA256 and imported Docker image ID before starting the application.
- The launchers never build the application or pull base images. Offline installation remains available by placing the image archive and `SHA256SUMS` in the extracted package's `docker-images/` directory.

The first online launch requires internet access and downloads approximately 500 MB. Docker Desktop or Docker Engine with Compose v2 is required. Windows x64 and Ubuntu x64 image packages are not included.

## Verification status

Release build, launcher packaging, archive checksums, Docker image identity, and host health check are recorded in the RC8 release ledger after publication. Windows and Ubuntu host execution are not claimed unless separately recorded.

## Rollback

Use the previous RC7 platform launcher and matching RC7 image assets. Do not remove or overwrite Docker volumes when rolling back.
