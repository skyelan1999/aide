#!/usr/bin/env bash
# Preserve a complete local dependency image independently of BuildKit cache.
set -euo pipefail
cd "$(dirname "$0")/.."
DOCKER_BIN="$(command -v docker || true)"
[[ -n "$DOCKER_BIN" ]] || DOCKER_BIN="$HOME/.docker/bin/docker"
BASE=aide:dependencies-v2
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
    archive="aide-dependencies-v2-${output_platform//\//-}.tar.gz"
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
if [[ "$("$DOCKER_BIN" image inspect "$BASE" --format '{{index .Config.Labels "aide.local-base"}}' 2>/dev/null || true)" == v2 ]]; then finish; exit 0; fi
PLATFORM="$(platform)"
ARCHIVE="aide-dependencies-v2-${PLATFORM//\//-}.tar.gz"
if [[ -f "docker-images/$ARCHIVE" ]]; then
  SUM="$(awk -v name="$ARCHIVE" '$2 == name {print $1}' docker-images/SHA256SUMS)"
  [[ "$SUM" =~ ^[0-9a-f]{64}$ ]] || { echo 'Local dependency archive checksum is missing or ambiguous.' >&2; exit 1; }
  ACTUAL="$(shasum -a 256 "docker-images/$ARCHIVE" | awk '{print $1}')"
  [[ "$SUM" == "$ACTUAL" ]] || { echo 'Local dependency archive checksum mismatch.' >&2; exit 1; }
  "$DOCKER_BIN" image load -i "docker-images/$ARCHIVE" >&2
  [[ "$("$DOCKER_BIN" image inspect "$BASE" --format '{{index .Config.Labels "aide.local-base"}}')" == v2 ]] || exit 1
  finish; exit 0
fi
SEED="${1:-aide:local}"
SEED_ID="$("$DOCKER_BIN" image inspect "$SEED" --format '{{.Id}}' 2>/dev/null || true)"
[[ "$SEED_ID" == sha256:* ]] || { echo 'No complete local AIDE image or dependency archive is available.' >&2; exit 3; }
# Never derive from an old CAD image: deleting files cannot remove inherited layer bytes.
if ! "$DOCKER_BIN" run --rm --network=none --entrypoint sh "$SEED_ID" -c 'test ! -e /opt/aide-cad && test ! -e /opt/aide/cad'; then
  echo 'Old CAD-containing image cannot seed lean dependencies; build once with AIDE_LOCAL_DEPENDENCIES=0.' >&2
  exit 3
fi
# A release runtime is deliberately not a source-build seed. Fail clearly and
# let the existing caller use its explicit source-development bootstrap path.
if ! "$DOCKER_BIN" run --rm --network=none --entrypoint sh "$SEED_ID" -c 'command -v go >/dev/null && command -v gcc >/dev/null && command -v npm >/dev/null'; then
  echo 'Slim runtime cannot seed source dependencies; use a development toolbox image or build the dev target.' >&2
  exit 3
fi
SEED_TAG="aide:dependency-seed-${SEED_ID#sha256:}"
"$DOCKER_BIN" image tag "$SEED_ID" "$SEED_TAG"
# Resolve only the locally tagged seed; RUN has no network access.
"$DOCKER_BIN" build --pull=false --network=none -f docker/Dockerfile.local-base --build-arg "AIDE_SEED_IMAGE=$SEED_TAG" -t "$BASE" . >&2
finish
