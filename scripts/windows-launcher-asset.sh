#!/usr/bin/env bash
# Resolve/build the Windows binary before a package can claim Windows support.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="${1:?container architecture required}"
IMAGE="${2:?local build image required}"
[[ "$ARCH" == amd64 || "$ARCH" == arm64 ]] || { echo 'Unsupported Windows architecture.' >&2; exit 1; }
if [[ -n "${AIDE_WINDOWS_LAUNCHER:-}" ]]; then
  BINARY="$AIDE_WINDOWS_LAUNCHER"
elif [[ -f "$ROOT/scripts/build-windows-launcher.py" && -d "$ROOT/native/windows-launcher" ]]; then
  python3 "$ROOT/scripts/build-windows-launcher.py" --image "$IMAGE" --arch "$ARCH" >&2
  if [[ "$ARCH" == amd64 ]]; then BINARY="$ROOT/aide-launcher.exe"; else BINARY="$ROOT/bin/aide-launcher-arm64.exe"; fi
else
  BINARY="$ROOT/aide-launcher.exe"
fi
[[ -f "$BINARY" && "$(od -An -tx1 -N2 "$BINARY" | tr -d ' \n')" == 4d5a ]] || { echo 'A matching native Windows launcher executable is required.' >&2; exit 1; }
python3 - "$BINARY" "$ARCH" <<'PY'
from pathlib import Path
import struct, sys
data=Path(sys.argv[1]).read_bytes()
offset=struct.unpack_from('<I',data,0x3c)[0]
expected=0x8664 if sys.argv[2]=='amd64' else 0xaa64
if data[offset:offset+4]!=b'PE\0\0' or struct.unpack_from('<H',data,offset+4)[0]!=expected:
    raise SystemExit('Windows launcher PE architecture does not match the package')
PY
printf '%s\n' "$BINARY"
