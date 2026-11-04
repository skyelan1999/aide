#!/usr/bin/env bash
# Experimental host-native Aide build. This build is independent of Docker.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
command -v go >/dev/null 2>&1 || { echo 'Go is required to build the native runtime.' >&2; exit 1; }
mkdir -p build/native
CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags='-s -w' -o build/native/aide ./cmd/aide
printf 'Built host-native Aide: %s\n' "$ROOT/build/native/aide"
