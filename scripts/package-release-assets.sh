#!/usr/bin/env bash
# Build platform launcher ZIPs plus one architecture-specific offline Docker image.
# The launchers are separated by host OS; the Linux container image is shared.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
TAG="${1:-v$(bash scripts/version.sh show | sed 's/ RC/-RC/')}"
IMAGE="${2:-aide:0.1.14.0-rc2-candidate}"
OUT="${3:-$ROOT/.agent-state/release-assets/$TAG}"
VERSION="${TAG#v}"
VERSION="${VERSION/-RC/ RC}"
DOCKER_BIN="$(command -v docker || true)"
[[ -n "$DOCKER_BIN" ]] || { echo 'docker is required.' >&2; exit 1; }
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"

IMAGE_ID="$("$DOCKER_BIN" image inspect "$IMAGE" --format '{{.Id}}')"
PLATFORM="$("$DOCKER_BIN" image inspect "$IMAGE" --format '{{.Os}}/{{.Architecture}}')"
[[ "$PLATFORM" == linux/arm64 || "$PLATFORM" == linux/amd64 ]] || { echo "Unsupported release platform: $PLATFORM" >&2; exit 1; }
source_sha() {
  (
    find cmd internal vendor scripts/office docker/wheels docker/sherpa -type f -print0
    printf '%s\0' go.mod go.sum Dockerfile compose.yaml .dockerignore version.md
  ) | sort -z | xargs -0 shasum -a 256 2>/dev/null | shasum -a 256 | awk '{print $1}'
}
IMAGE_SRC_SHA="$("$DOCKER_BIN" image inspect "$IMAGE" --format '{{index .Config.Labels "aide.srcsha"}}')"
EXPECTED_SRC_SHA="$(source_sha)"
[[ "$IMAGE_SRC_SHA" == "$EXPECTED_SRC_SHA" ]] || { echo "Image source label mismatch: $IMAGE_SRC_SHA != $EXPECTED_SRC_SHA. Build from this exact tag first." >&2; exit 1; }
SHORT_ID="${IMAGE_ID#sha256:}"
SHORT_ID="${SHORT_ID:0:12}"
IMAGE_ASSET="aide-${TAG}-linux-${PLATFORM#linux/}-image.tar.gz"
if [[ ! -f "$OUT/$IMAGE_ASSET" ]]; then
  echo "== Exporting $IMAGE ($PLATFORM, $IMAGE_ID) =="
  "$DOCKER_BIN" image save "$IMAGE" | gzip -1 > "$OUT/$IMAGE_ASSET.partial"
  mv "$OUT/$IMAGE_ASSET.partial" "$OUT/$IMAGE_ASSET"
fi
gzip -t "$OUT/$IMAGE_ASSET"

STAGING="$(mktemp -d "$ROOT/.agent-state/release-package.XXXXXX")"
trap 'rm -rf "$STAGING"' EXIT
for TARGET in macos-arm64 windows-arm64 ubuntu-arm64; do
  case "$TARGET" in
    macos-arm64) LAUNCHERS=(start.command);;
    windows-arm64) LAUNCHERS=(start.bat start.ps1);;
    ubuntu-arm64) LAUNCHERS=(start.sh);;
  esac
  PACKAGE="$STAGING/aide-$TAG-$TARGET"
  mkdir -p "$PACKAGE/scripts" "$PACKAGE/docker-images" "$PACKAGE/workspace" "$PACKAGE/context"
  for launcher in "${LAUNCHERS[@]}"; do cp "$ROOT/$launcher" "$PACKAGE/"; done
  cp "$ROOT/scripts/aide.sh" "$PACKAGE/scripts/"
  cp "$ROOT/scripts/watch-port.sh" "$PACKAGE/scripts/"
  cp "$ROOT/docker/compose.offline.yaml" "$PACKAGE/compose.yaml"
  cp "$ROOT/docker/offline.env.example" "$PACKAGE/.env.example"
  cp "$ROOT/LICENSE" "$PACKAGE/"
  printf '%s %s %s\n' "$IMAGE" "$IMAGE_ID" "$PLATFORM" > "$PACKAGE/.aide-image"
  cat > "$PACKAGE/README.txt" <<EOF
aide $VERSION - $TARGET launcher package

1. Install Docker Desktop (macOS/Windows) or Docker Engine with Compose v2 (Ubuntu).
2. Download the matching asset named: $IMAGE_ASSET
3. Copy the image file into this folder's docker-images directory, keeping its filename.
4. Copy the Release SHA256SUMS file into docker-images/SHA256SUMS.
5. Run the platform launcher in this folder. It verifies/imports the image and starts aide without building or pulling.
6. On first launch, the script uses port 8097 when available and selects/saves the next available port if it is occupied. Change the host port in Settings > Accessibility; the running launcher monitor applies it by recreating the container.

Required Docker engine image platform: $PLATFORM
Image reference: $IMAGE
Image ID: $IMAGE_ID

This launcher archive does not contain your .env, workspace data, API key, browser token, or Docker volumes.
Configure model access in the app after first start. Default local URL: https://localhost:8097
EOF
  chmod +x "$PACKAGE/start.command" "$PACKAGE/start.sh" "$PACKAGE/scripts/"*.sh 2>/dev/null || true
  (cd "$STAGING" && zip -qr "$OUT/aide-$TAG-$TARGET.zip" "aide-$TAG-$TARGET")
done

cat > "$OUT/RELEASE-ASSETS.txt" <<EOF
aide $VERSION release assets
Docker image: $IMAGE_ASSET ($PLATFORM, $IMAGE_ID)
Launchers: macOS Apple Silicon, Windows ARM64, Ubuntu ARM64
Windows x64 and Ubuntu x64 image bundles are omitted because no local linux/amd64 candidate image is available.
EOF
(cd "$OUT" && shasum -a 256 "$IMAGE_ASSET" ./*.zip RELEASE-ASSETS.txt > SHA256SUMS)
echo "Created release assets in $OUT"
ls -lh "$OUT"
