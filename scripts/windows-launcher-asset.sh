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
python3 - "$BINARY" "$ARCH" <<'PY_GATE'
from pathlib import Path
import struct,sys
binary=Path(sys.argv[1])
if binary.stat().st_size > 16*1024*1024: raise SystemExit('Windows launcher exceeds package size limit')
data=binary.read_bytes()
if len(data)<64 or data[:2]!=b'MZ': raise SystemExit('Malformed Windows launcher PE')
offset=struct.unpack_from('<I',data,0x3c)[0]
if offset<64 or offset>len(data)-24 or data[offset:offset+4]!=b'PE\0\0': raise SystemExit('Malformed Windows launcher PE')
expected=0x8664 if sys.argv[2]=='amd64' else 0xaa64
if struct.unpack_from('<H',data,offset+4)[0]!=expected: raise SystemExit('Windows launcher PE architecture does not match package')
PY_GATE
printf '%s\n' "$BINARY"
