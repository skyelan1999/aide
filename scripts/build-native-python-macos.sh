#!/usr/bin/env bash
# Prepare the pinned relocatable Python runtime used by the experimental macOS package.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${AIDE_NATIVE_PYTHON_OUT:-$ROOT/build/native/python}"
VERSION=3.12.16
RELEASE=20261026
case "$(uname -m)" in
  arm64) ARCH=aarch64-apple-darwin; SHA256=fbf4afef53c2a6b72bb741bc06d5671a9dfaf87bd84e5aa73ac9b3d0d589f14e ;;
  x86_64) ARCH=x86_64-apple-darwin; SHA256=a962e47df522889eaa7ff0d6a56203f1079f68aabb65ffdbe8455f362bc769f3 ;;
  *) echo "Unsupported macOS architecture: $(uname -m)" >&2; exit 2 ;;
esac
ASSET="cpython-${VERSION}+${RELEASE}-${ARCH}-install_only.tar.gz"
URL="https://github.com/astral-sh/python-build-standalone/releases/download/${RELEASE}/${ASSET//+/%2B}"
mkdir -p "$(dirname "$OUT")"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-python.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$WORK"' EXIT
curl -fL --retry 2 --connect-timeout 15 --max-time 240 "$URL" -o "$WORK/python.tar.gz"
printf '%s  %s\n' "$SHA256" "$WORK/python.tar.gz" | shasum -a 256 -c -
mkdir "$WORK/unpacked"
tar -xzf "$WORK/python.tar.gz" -C "$WORK/unpacked"
PY="$WORK/unpacked/python/bin/python3.12"
"$PY" -m pip install --disable-pip-version-check --no-warn-script-location \
  --target "$WORK/unpacked/python/lib/python3.12/site-packages" \
  python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4 'pypdf[crypto]==6.19.0'
PYTHONPATH="$WORK/unpacked/python/lib/python3.12/site-packages" "$PY" -c \
  'import docx,openpyxl,pptx,ezdxf,pypdf; assert pypdf.__version__ == "6.19.0"; print("Python Office runtime ready")'
if [[ -e "$OUT" ]]; then
  echo "Output already exists: $OUT (move it aside before rebuilding)." >&2
  exit 1
fi
mv "$WORK/unpacked/python" "$OUT"
printf 'Prepared Python runtime: %s\n' "$OUT"
