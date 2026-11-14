#!/bin/sh
# Build a separate readonly DWG-to-DXF CLI; never link it into the Aide binary.
set -eu
destination=${1:-/opt/aide/dwg}
metadata=${2:-docker/libredwg/source.json}
cache=${AIDE_DWG_SOURCE_CACHE:-/root/.cache/aide/libredwg}
version=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["version"])' "$metadata")
url=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["url"])' "$metadata")
digest=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["sha256"])' "$metadata")
mkdir -p "$cache" "$destination/bin" "$destination/source"
archive="$cache/libredwg-$version.tar.xz"
if ! printf '%s  %s\n' "$digest" "$archive" | sha256sum -c - >/dev/null 2>&1; then
  curl -fSL --retry 3 --connect-timeout 15 --max-time 180 "$url" -o "$archive.partial"
  printf '%s  %s\n' "$digest" "$archive.partial" | sha256sum -c -
  mv "$archive.partial" "$archive"
fi
build=$(mktemp -d /tmp/aide-libredwg-build.XXXXXX)
trap 'rm -rf "$build"' EXIT HUP INT TERM
tar -xf "$archive" -C "$build"
cd "$build/libredwg-$version"
CFLAGS='-O1 -g0' ./configure --disable-shared --enable-static --disable-write \
  --disable-json --disable-bindings --disable-python --disable-docs --disable-dependency-tracking
make -C src -j2
make -C programs -j2 dwg2dxf
install -m 0755 programs/dwg2dxf "$destination/bin/dwg2dxf"
strip "$destination/bin/dwg2dxf"
cp COPYING AUTHORS "$destination/"
cp "$archive" "$destination/source/"
printf '%s  libredwg-%s.tar.xz\n' "$digest" "$version" > "$destination/source/SHA256SUMS"
"$destination/bin/dwg2dxf" --version
