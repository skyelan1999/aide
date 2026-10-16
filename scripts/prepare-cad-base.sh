#!/usr/bin/env bash
# Reuse a retained CAD runtime or recover it from an imported application image.
# Missing dependencies are explicit; never silently download during local build.
set -euo pipefail
cd "$(dirname "$0")/.."
DOCKER_BIN="$(command -v docker || true)"
[[ -n "$DOCKER_BIN" ]] || DOCKER_BIN="$HOME/.docker/bin/docker"
BASE=aide:cad-dependencies-v1
check_platform() {
 local engine image
 engine="$("$DOCKER_BIN" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|')"
 image="$("$DOCKER_BIN" image inspect "$1" --format '{{.Os}}/{{.Architecture}}')"
 [[ "$image" == "$engine" ]] || { echo "CAD dependency platform mismatch: $image vs $engine" >&2; return 1; }
}
if [[ "$("$DOCKER_BIN" image inspect "$BASE" --format '{{index .Config.Labels "aide.cad-runtime"}}' 2>/dev/null || true)" == 8.0.1.1.0 ]]; then
 check_platform "$BASE"
 echo "$BASE";exit 0
fi
SEED="${1:-aide:local}"
if [[ "$("$DOCKER_BIN" image inspect "$SEED" --format '{{index .Config.Labels "aide.cad-runtime"}}' 2>/dev/null || true)" == 8.0.1.1.0 ]]; then
 check_platform "$SEED"
 "$DOCKER_BIN" image tag "$SEED" "$BASE"
 echo "$BASE";exit 0
fi
echo 'Native CAD runtime is missing. Bootstrap once: docker build -f docker/Dockerfile.cad -t aide:cad-dependencies-v1 .' >&2
exit 3
