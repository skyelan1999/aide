#!/usr/bin/env bash
# Preserve a complete local dependency image independently of BuildKit cache.
set -euo pipefail
cd "$(dirname "$0")/.."
DOCKER_BIN="$(command -v docker || true)"
[[ -n "$DOCKER_BIN" ]] || DOCKER_BIN="$HOME/.docker/bin/docker"
BASE=aide:dependencies-v1
EXPORT=0
if [[ "${1:-}" == --export ]]; then EXPORT=1; shift; fi
platform() { "$DOCKER_BIN" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|'; }
finish() {
  local engine_platform image_platform
  engine_platform="$(platform)"
  image_platform="$("$DOCKER_BIN" image inspect "$BASE" --format '{{.Os}}/{{.Architecture}}')"
  [[ "$image_platform" == "$engine_platform" ]] || { echo "Local dependency image platform mismatch: $image_platform vs $engine_platform" >&2; return 1; }
  if (( EXPORT )); then
    local output_platform archive sum manifest_tmp
    output_platform="$(platform)"
    archive="aide-dependencies-v1-${output_platform//\//-}.tar.gz"
    mkdir -p docker-images
    # Export only image layers; no container, volume or host data enters it.
    "$DOCKER_BIN" image save "$BASE" | gzip -1 > "docker-images/$archive.partial"
    mv "docker-images/$archive.partial" "docker-images/$archive"
    sum="$(shasum -a 256 "docker-images/$archive" | awk '{print $1}')"
    manifest_tmp="$(mktemp docker-images/checksums.XXXXXX)"
    if [[ -f docker-images/SHA256SUMS ]]; then awk -v name="$archive" '$2 != name' docker-images/SHA256SUMS > "$manifest_tmp"; fi
    printf '%s  %s\n' "$sum" "$archive" >> "$manifest_tmp"
    mv "$manifest_tmp" docker-images/SHA256SUMS
    echo "Local dependency archive: docker-images/$archive ($sum)" >&2
  fi
  echo "$BASE"
}
if [[ "$("$DOCKER_BIN" image inspect "$BASE" --format '{{index .Config.Labels "aide.local-base"}}' 2>/dev/null || true)" == v1 ]]; then finish; exit 0; fi
PLATFORM="$(platform)"
ARCHIVE="aide-dependencies-v1-${PLATFORM//\//-}.tar.gz"
if [[ -f "docker-images/$ARCHIVE" ]]; then
  SUM="$(awk -v name="$ARCHIVE" '$2 == name {print $1}' docker-images/SHA256SUMS)"
  [[ "$SUM" =~ ^[0-9a-f]{64}$ ]] || { echo 'Local dependency archive checksum is missing or ambiguous.' >&2; exit 1; }
  ACTUAL="$(shasum -a 256 "docker-images/$ARCHIVE" | awk '{print $1}')"
  [[ "$SUM" == "$ACTUAL" ]] || { echo 'Local dependency archive checksum mismatch.' >&2; exit 1; }
  "$DOCKER_BIN" image load -i "docker-images/$ARCHIVE" >&2
  [[ "$("$DOCKER_BIN" image inspect "$BASE" --format '{{index .Config.Labels "aide.local-base"}}')" == v1 ]] || exit 1
  finish; exit 0
fi
SEED="${1:-aide:local}"
SEED_ID="$("$DOCKER_BIN" image inspect "$SEED" --format '{{.Id}}' 2>/dev/null || true)"
[[ "$SEED_ID" == sha256:* ]] || { echo 'No complete local AIDE image or dependency archive is available.' >&2; exit 3; }
SEED_TAG="aide:dependency-seed-${SEED_ID#sha256:}"
"$DOCKER_BIN" image tag "$SEED_ID" "$SEED_TAG"
# Resolve only the locally tagged seed; RUN has no network access.
"$DOCKER_BIN" build --pull=false --network=none -f docker/Dockerfile.local-base --build-arg "AIDE_SEED_IMAGE=$SEED_TAG" -t "$BASE" . >&2
finish
