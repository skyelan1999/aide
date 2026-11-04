#!/usr/bin/env bash
# Prepare a relocatable Windows x64 Python 3.12 runtime and pinned Office wheels.
set -euo pipefail
OUT="${AIDE_NATIVE_PYTHON_OUT:-$(cd "$(dirname "$0")/.." && pwd)/build/native/windows/python}"
VERSION=3.12.16
RELEASE=20261026
ASSET="cpython-${VERSION}+${RELEASE}-x86_64-pc-windows-msvc-install_only.tar.gz"
SHA256=590dfcbe3a34c07301e6332e13e2188b98655cd072b08afa45a7998dc4c22bc5
URL="https://github.com/astral-sh/python-build-standalone/releases/download/${RELEASE}/${ASSET//+/%2B}"
command -v curl >/dev/null && command -v python3 >/dev/null || { echo 'curl and Python 3 are required.' >&2; exit 1; }
[[ ! -e "$OUT" ]] || { echo "Output already exists: $OUT" >&2; exit 1; }
mkdir -p "$(dirname "$OUT")"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-python-windows.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$WORK"' EXIT
curl -fL --retry 2 --connect-timeout 15 --max-time 240 "$URL" -o "$WORK/python.tar.gz"
printf '%s  %s\n' "$SHA256" "$WORK/python.tar.gz" | shasum -a 256 -c -
mkdir "$WORK/unpacked"
tar -xzf "$WORK/python.tar.gz" -C "$WORK/unpacked"
PYROOT="$WORK/unpacked/python"
[[ -f "$PYROOT/python.exe" ]] || { echo 'Standalone Python archive did not contain python.exe.' >&2; exit 1; }
find "$PYROOT" -type f -name '*.pdb' -delete
# Resolve and download Windows wheels from PyPI without executing Windows code.
python3 -m pip install --disable-pip-version-check --no-warn-script-location \
  --target "$PYROOT/Lib/site-packages" --only-binary=:all: \
  --platform win_amd64 --python-version 3.12 --implementation cp --abi cp312 \
  --ignore-requires-python \
  python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4 'pypdf[crypto]==6.19.0'
[[ ! -e "$OUT" ]] || { echo "Output already exists: $OUT" >&2; exit 1; }
mkdir -p "$(dirname "$OUT")"
mv "$PYROOT" "$OUT"
printf 'Prepared Windows Python runtime: %s\n' "$OUT"
