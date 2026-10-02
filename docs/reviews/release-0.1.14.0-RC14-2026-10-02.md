# aide 0.1.14.0 RC14

## macOS launcher

The lightweight macOS launcher now retries and resumes interrupted GitHub Release image downloads. It verifies the downloaded image against the release `SHA256SUMS` before importing it. After repeated failures, `start.command` leaves the partial download available for another run and prints a recovery message. The browser is opened after the image is verified, the service starts, and its health check passes.

The full package remains available for offline installation and can be selected as an extracted folder for in-app updates.

## Verification

- The interrupted-transfer fixture verified resume, SHA256 validation, image import, and invocation of the macOS browser opener.
- `python3 scripts/agent-route.py verify quick` passed.
- `python3 scripts/agent-route.py verify full` passed, including Docker `go test -race` and `go vet`.
- Direct network behavior against the public RC14 asset is not verified until the release is published.
