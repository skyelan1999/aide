#!/bin/sh
# Called inside the existing image build; no additional network or host compiler.
set -eu
out="${1:-/opt/aide-message-lan}"
mkdir -p "$out"
for platform in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64 windows/arm64; do
  target_os=${platform%/*}; target_arch=${platform#*/}; suffix=''
  if [ "$target_os" = windows ]; then suffix=.exe; fi
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -mod=vendor -trimpath -ldflags='-s -w' -o "$out/aide-message-lan-$target_os-$target_arch$suffix" ./cmd/aide-message-lan
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -mod=vendor -trimpath -ldflags='-s -w' -o "$out/aide-local-mounts-$target_os-$target_arch$suffix" ./cmd/aide-local-mounts
done
(cd "$out" && sha256sum aide-message-lan-* aide-local-mounts-* > SHA256SUMS)
