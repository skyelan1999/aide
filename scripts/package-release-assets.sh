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
IMAGE_SRC_SHA="$("$DOCKER_BIN" image inspect "$IMAGE" --format '{{index .Config.Labels "aide.srcsha"}}')"
EXPECTED_SRC_SHA="$(bash scripts/aide.sh source-sha)"
[[ "$IMAGE_SRC_SHA" == "$EXPECTED_SRC_SHA" ]] || { echo "Image source label mismatch: $IMAGE_SRC_SHA != $EXPECTED_SRC_SHA. Build from this exact tag first." >&2; exit 1; }
SHORT_ID="${IMAGE_ID#sha256:}"
SHORT_ID="${SHORT_ID:0:12}"
IMAGE_ASSET="aide-${TAG}-linux-${PLATFORM#linux/}-image.tar.gz"
WINDOWS_LAUNCHER="$(bash "$ROOT/scripts/windows-launcher-asset.sh" "${PLATFORM#linux/}" "$IMAGE")"
echo "== Exporting $IMAGE ($PLATFORM, $IMAGE_ID) =="
"$DOCKER_BIN" image save "$IMAGE" | gzip -1 > "$OUT/$IMAGE_ASSET.partial"
mv "$OUT/$IMAGE_ASSET.partial" "$OUT/$IMAGE_ASSET"
gzip -t "$OUT/$IMAGE_ASSET"

# The in-app updater accepts one self-contained, checksummed platform bundle.
mkdir -p "$ROOT/.agent-state"
copy_control_runtime() {
  local destination="$1"
  cp "$ROOT/scripts/safari-bridge-control.sh" "$ROOT/scripts/safari-bridge.js" \
     "$ROOT/scripts/computer-bridge-control.sh" "$ROOT/scripts/computer-bridge.js" \
     "$ROOT/scripts/build-native-computer-bridge.sh" "$ROOT/scripts/native-computer-bridge.swift" \
     "$ROOT/scripts/headless-browser-control.sh" "$ROOT/scripts/headless-browser-bridge.js" "$ROOT/scripts/browser-url-policy.cjs" "$destination/"
  mkdir -p "$destination/browser-runtime"
  cp "$ROOT/scripts/browser-runtime/package.json" "$ROOT/scripts/browser-runtime/package-lock.json" "$destination/browser-runtime/"
}

UPDATE_STAGING="$(mktemp -d "$ROOT/.agent-state/update-package.XXXXXX")"
cp "$OUT/$IMAGE_ASSET" "$UPDATE_STAGING/"
(cd "$UPDATE_STAGING" && shasum -a 256 "$IMAGE_ASSET" > SHA256SUMS)
printf '{"format":"aide-update-package","version":1,"releaseTag":"%s","platform":"%s","imageArchive":"%s","imageId":"%s"}\n' \
  "$TAG" "$PLATFORM" "$IMAGE_ASSET" "$IMAGE_ID" > "$UPDATE_STAGING/manifest.json"
(cd "$UPDATE_STAGING" && zip -q "$OUT/aide-$TAG-update-${PLATFORM//\//-}.zip" manifest.json SHA256SUMS "$IMAGE_ASSET")
rm -rf "$UPDATE_STAGING"

STAGING="$(mktemp -d "$ROOT/.agent-state/release-package.XXXXXX")"
trap 'rm -rf "$STAGING"' EXIT
for TARGET in macos-${PLATFORM#linux/} windows-${PLATFORM#linux/} ubuntu-${PLATFORM#linux/}; do
  case "$TARGET" in
    macos-*) LAUNCHERS=(start.command stop.command clean-docker-cache.command);;
    windows-*) LAUNCHERS=(start.bat stop.bat clean-docker-cache.bat);;
    ubuntu-*) LAUNCHERS=(start.sh stop.sh clean-docker-cache.sh);;
  esac
  PACKAGE="$STAGING/aide-$TAG-$TARGET"
  mkdir -p "$PACKAGE/scripts" "$PACKAGE/docker-images" "$PACKAGE/workspace" "$PACKAGE/context"
  for launcher in "${LAUNCHERS[@]}"; do cp "$ROOT/$launcher" "$PACKAGE/"; done
  if [[ "$TARGET" == windows-* ]]; then cp "$WINDOWS_LAUNCHER" "$PACKAGE/aide-launcher.exe"; fi
  if [[ "$TARGET" != windows-* ]]; then
    cp "$ROOT/scripts/aide-maintenance.sh" "$ROOT/scripts/local-mounts-control.sh" "$ROOT/scripts/aide.sh" "$ROOT/scripts/message-lan-control.sh" "$PACKAGE/scripts/"
    cp "$ROOT/scripts/launcher-data.sh" "$ROOT/scripts/launcher-data.py" "$ROOT/scripts/prebuilt-image.sh" "$ROOT/scripts/resource-guard.sh" "$ROOT/scripts/watch-port.sh" "$ROOT/scripts/update-agent.sh" "$PACKAGE/scripts/"
  fi
  cp "$ROOT/docker/compose.offline.yaml" "$PACKAGE/compose.yaml"
  cp "$ROOT/docker/offline.env.example" "$PACKAGE/.env.example"
  cp "$ROOT/scripts/codex-host-control.cjs" "$PACKAGE/scripts/"
  copy_control_runtime "$PACKAGE/scripts"
  cp "$ROOT/LICENSE" "$PACKAGE/"
  printf 'aide:slot-a %s %s %s\n' "$IMAGE_ID" "$PLATFORM" "$TAG" > "$PACKAGE/.aide-image"
  cat > "$PACKAGE/README.txt" <<EOF
aide $VERSION - $TARGET launcher package

English documentation: https://github.com/skyelan1999/aide/blob/$TAG/docs/en/README.md
简体中文文档: https://github.com/skyelan1999/aide/blob/$TAG/docs/README.md

1. Install Docker Desktop (macOS/Windows) or Docker Engine with Compose v2 (Ubuntu).
2. Run the platform launcher in this folder. If the image is not already present, it downloads the matching Release image and SHA256SUMS, verifies the SHA256 and image ID, then imports and starts aide without building or pulling.
3. The first launch downloads about 500 MB and requires internet access. To prepare an offline install, download $IMAGE_ASSET and SHA256SUMS and place both in docker-images/ before launch.
4. On first launch, the script uses port 8097 when available and selects/saves the next available port if it is occupied. Change the host port in Settings > Accessibility; the running launcher monitor applies it by recreating the container.
5. macOS first launch: this ZIP is not yet signed with an Apple Developer ID or notarized, so Gatekeeper may block start.command. Only if you verified this package came from the official aide Release and is unchanged, try opening it once, then go to System Settings > Privacy & Security > Security and choose Open Anyway. If macOS does not offer that button, open Terminal in this package folder and run: xattr -d com.apple.quarantine start.command. Then double-click start.command. Do not do this for software from an untrusted source.

Required Docker engine image platform: $PLATFORM
Image reference: $IMAGE
Image ID: $IMAGE_ID

This launcher archive does not contain your .env, workspace data, API key, browser token, or Docker volumes.
Configure model access in the app after first start. Default local URL: https://localhost:8097
EOF
  chmod +x "$PACKAGE/start.command" "$PACKAGE/start.sh" "$PACKAGE/scripts/"*.sh 2>/dev/null || true
  (cd "$STAGING" && zip -qr "$OUT/aide-$TAG-$TARGET.zip" "aide-$TAG-$TARGET")
done

# One ZIP serves both first-run bootstrap and in-app A/B upgrade. It contains
# all host launchers plus a single copy of the image and the exact payload
# triple the updater extracts from either this ZIP or its macOS-unzipped folder.
FULL_TARGET="aide-$TAG-full-linux-${PLATFORM#linux/}"
FULL_PACKAGE="$STAGING/$FULL_TARGET"
mkdir -p "$FULL_PACKAGE/scripts" "$FULL_PACKAGE/docker-images" "$FULL_PACKAGE/workspace" "$FULL_PACKAGE/context"
cp "$ROOT"/stop.{command,sh,bat} "$ROOT"/clean-docker-cache.{command,sh,bat} "$FULL_PACKAGE/"
cp "$ROOT/scripts/aide-maintenance.sh" "$FULL_PACKAGE/scripts/"
cp "$ROOT/scripts/local-mounts-control.sh" "$ROOT/scripts/message-lan-control.sh" "$FULL_PACKAGE/scripts/"
cp "$ROOT/start.command" "$ROOT/start.sh" "$ROOT/start.bat" "$FULL_PACKAGE/"
cp "$WINDOWS_LAUNCHER" "$FULL_PACKAGE/aide-launcher.exe"
cp "$ROOT/scripts/windows-launcher-asset.sh" "$FULL_PACKAGE/scripts/"
cp "$ROOT/scripts/aide.sh" "$ROOT/scripts/watch-port.sh" "$ROOT/scripts/update-agent.sh" "$FULL_PACKAGE/scripts/"
cp "$ROOT/scripts/launcher-data.sh" "$ROOT/scripts/launcher-data.py" "$ROOT/scripts/prebuilt-image.sh" "$ROOT/scripts/resource-guard.sh" "$FULL_PACKAGE/scripts/"
cp "$ROOT/docker/compose.offline.yaml" "$FULL_PACKAGE/compose.yaml"
cp "$ROOT/docker/offline.env.example" "$FULL_PACKAGE/.env.example"
cp "$ROOT/scripts/codex-host-control.cjs" "$FULL_PACKAGE/scripts/"
copy_control_runtime "$FULL_PACKAGE/scripts"
cp "$ROOT/LICENSE" "$FULL_PACKAGE/"
cp "$OUT/$IMAGE_ASSET" "$FULL_PACKAGE/docker-images/"
printf '%s %s %s %s\n' "aide:slot-a" "$IMAGE_ID" "$PLATFORM" "$TAG" > "$FULL_PACKAGE/.aide-image"
(cd "$FULL_PACKAGE/docker-images" && shasum -a 256 "$IMAGE_ASSET" > SHA256SUMS)
printf '{"format":"aide-update-package","version":1,"releaseTag":"%s","platform":"%s","imageArchive":"%s","imageId":"%s"}\n' \
  "$TAG" "$PLATFORM" "$IMAGE_ASSET" "$IMAGE_ID" > "$FULL_PACKAGE/docker-images/manifest.json"
cat > "$FULL_PACKAGE/README.txt" <<EOF
aide $VERSION - complete runtime and upgrade package

English documentation: https://github.com/skyelan1999/aide/blob/$TAG/docs/en/README.md
简体中文文档: https://github.com/skyelan1999/aide/blob/$TAG/docs/README.md

This single ZIP supports both a new installation and an in-app A/B upgrade.
It includes the platform launchers and one verified Docker image archive.

First run: extract this folder, then run start.command (macOS), aide-launcher.exe or start.bat (Windows), or start.sh (Ubuntu). The launcher verifies and imports the included image without downloading it, then starts aide. Docker Desktop / Docker Engine with Compose v2 is required.

Upgrade an existing Release installation: in aide open Settings > Software Update and select this same ZIP. If macOS automatically extracts it, choose the extracted package folder instead. The updater locates docker-images/manifest.json, SHA256SUMS and the image archive, ignores launcher/workspace files, validates the package, and stages it in the inactive slot. Confirm the manual activation in Settings to switch.

Required Docker engine image platform: $PLATFORM
Image reference: aide:slot-a
Image ID: $IMAGE_ID

This package does not contain .env, workspace contents, API keys, browser tokens, or Docker volumes. A/B slots share the existing aide data volumes.
EOF
chmod +x "$FULL_PACKAGE/start.command" "$FULL_PACKAGE/start.sh" "$FULL_PACKAGE/scripts/"*.sh
(cd "$STAGING" && zip -0qr "$OUT/aide-$TAG-full-linux-${PLATFORM#linux/}.zip" "$FULL_TARGET")

# Also ship the complete archive referenced by ordinary source checkouts.
AIDE_IMAGE="$IMAGE" bash scripts/aide.sh export-bundle
SNAPSHOT="aide-$(bash scripts/version.sh show | tr ' ' '-')-${PLATFORM//\//-}-$SHORT_ID-offline.tar.gz"
cp "$ROOT/docker-images/$SNAPSHOT" "$OUT/$SNAPSHOT"

cat > "$OUT/RELEASE-ASSETS.txt" <<EOF
aide $VERSION release assets
Docker image: $IMAGE_ASSET ($PLATFORM, $IMAGE_ID)
Complete checkout bootstrap archive: $SNAPSHOT
Launchers: macOS, Windows, Ubuntu (${PLATFORM#linux/} Docker engine)
Unified full runtime + upgrade package: aide-$TAG-full-linux-${PLATFORM#linux/}.zip (all launchers and one image archive; use the same ZIP for first run or the in-app updater)
This asset set contains only the stated Docker engine architecture. Build and verify the other architecture separately before advertising it.
EOF
(cd "$OUT" && shasum -a 256 "$IMAGE_ASSET" "$SNAPSHOT" ./*.zip RELEASE-ASSETS.txt > SHA256SUMS)
echo "Created release assets in $OUT"
ls -lh "$OUT"
