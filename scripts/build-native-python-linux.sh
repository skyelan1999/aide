#!/usr/bin/env bash
# Build the pinned, relocatable Linux Python runtime for the experimental .deb.
set -euo pipefail
OUT="${AIDE_NATIVE_PYTHON_OUT:-$(cd "$(dirname "$0")/.." && pwd)/build/native/python}"
VERSION=3.12.16
RELEASE=20261026
case "$(uname -m)" in
  aarch64|arm64) ARCH=aarch64-unknown-linux-gnu; SHA256=a945427411dc6851a1393533b74a4a1ac434321ae397f2ec69897a9b4fde032b ;;
  x86_64|amd64) ARCH=x86_64-unknown-linux-gnu; SHA256=6a79e922e50006b601f29e688407659da1c2b86ebcd4e9c7254cf234be8b89de ;;
  *) echo "Unsupported Linux architecture: $(uname -m)" >&2; exit 2 ;;
esac
ASSET="cpython-${VERSION}+${RELEASE}-${ARCH}-install_only.tar.gz"
URL="https://github.com/astral-sh/python-build-standalone/releases/download/${RELEASE}/${ASSET//+/%2B}"
mkdir -p "$(dirname "$OUT")"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-python-linux.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$WORK"' EXIT
curl -fL --retry 2 --connect-timeout 15 --max-time 240 "$URL" -o "$WORK/python.tar.gz"
printf '%s  %s\n' "$SHA256" "$WORK/python.tar.gz" | sha256sum -c -
mkdir "$WORK/unpacked"
tar -xzf "$WORK/python.tar.gz" -C "$WORK/unpacked"
PY="$WORK/unpacked/python/bin/python3.12"
"$PY" -m pip install --disable-pip-version-check --no-warn-script-location \
  --target "$WORK/unpacked/python/lib/python3.12/site-packages" \
  python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4 'pypdf[crypto]==6.19.0'
PYTHONPATH="$WORK/unpacked/python/lib/python3.12/site-packages" "$PY" -I -c \
  'import docx,openpyxl,pptx,ezdxf,pypdf; assert pypdf.__version__ == "6.19.0"; print("Python Office runtime ready")'
if [[ -e "$OUT" ]]; then echo "Output already exists: $OUT" >&2; exit 1; fi
mv "$WORK/unpacked/python" "$OUT"
printf 'Prepared Python runtime: %s\n' "$OUT"
