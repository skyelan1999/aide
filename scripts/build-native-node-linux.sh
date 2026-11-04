#!/usr/bin/env bash
# Build a verified Node 24 runtime for the experimental Linux package.
set -euo pipefail
OUT="${AIDE_NATIVE_NODE_OUT:-$(cd "$(dirname "$0")/.." && pwd)/build/native/node}"
VERSION=24.19.0
case "$(uname -m)" in
  aarch64|arm64) ARCH=arm64; SHA256=01443c1e1a29e531ccad5a46fefa6df490d2189c49f7955904aecdbb0fe86fdc ;;
  x86_64|amd64) ARCH=x64; SHA256=14b342e71204f811bde6153be8e04b62aef63c236fef92b55f9c83154b409647 ;;
  *) echo "Unsupported Linux architecture: $(uname -m)" >&2; exit 2 ;;
esac
ASSET="node-v${VERSION}-linux-${ARCH}.tar.xz"
URL="https://nodejs.org/dist/v${VERSION}/${ASSET}"
mkdir -p "$(dirname "$OUT")"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-node-linux.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$WORK"' EXIT
curl -fL --retry 2 --connect-timeout 15 --max-time 240 "$URL" -o "$WORK/node.tar.xz"
printf '%s  %s\n' "$SHA256" "$WORK/node.tar.xz" | sha256sum -c -
tar -xJf "$WORK/node.tar.xz" -C "$WORK"
SOURCE="$WORK/node-v${VERSION}-linux-${ARCH}"
mkdir -p "$WORK/runtime/bin" "$WORK/runtime/lib/node_modules"
cp "$SOURCE/bin/node" "$WORK/runtime/bin/node"
cp -R "$SOURCE/lib/node_modules/npm" "$WORK/runtime/lib/node_modules/npm"
ln -s ../lib/node_modules/npm/bin/npm-cli.js "$WORK/runtime/bin/npm"
ln -s ../lib/node_modules/npm/bin/npx-cli.js "$WORK/runtime/bin/npx"
"$WORK/runtime/bin/node" --version
PATH="$WORK/runtime/bin:$PATH" "$WORK/runtime/bin/npm" --version
if [[ -e "$OUT" ]]; then echo "Output already exists: $OUT" >&2; exit 1; fi
mv "$WORK/runtime" "$OUT"
printf 'Prepared Node runtime: %s\n' "$OUT"
