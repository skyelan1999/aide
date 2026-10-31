#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${AIDE_NATIVE_BRIDGE_APP:-${HOME}/Library/Application Support/Aide/Aide Computer Bridge.app}"
mkdir -p "$DEST/Contents/MacOS"
mkdir -p "$DEST/Contents/Resources"
xcrun swiftc -swift-version 5 -module-cache-path "$ROOT/.cache/swift-modules" -target "$(uname -m)-apple-macos14.0" -O "$ROOT/scripts/native-computer-bridge.swift" -o "$DEST/Contents/MacOS/AideComputerBridge"
ICONSET="${TMPDIR:-/tmp}/aide-computer-bridge-iconset-$$.iconset"
ICON_RENDERER="${TMPDIR:-/tmp}/aide-computer-bridge-icon-renderer-$$"
trap 'rm -rf "$ICONSET"; rm -f "$ICON_RENDERER"' EXIT
mkdir "$ICONSET"
xcrun swiftc -swift-version 5 -module-cache-path "$ROOT/.cache/swift-modules" \
  "$ROOT/scripts/render-computer-bridge-icon.swift" -o "$ICON_RENDERER"
"$ICON_RENDERER" "$ICONSET"
python3 - "$ICONSET" "$DEST/Contents/Resources/AppIcon.icns" <<'PY'
import struct, sys
from pathlib import Path

iconset, output = Path(sys.argv[1]), Path(sys.argv[2])
variants = [
    ("icp4", "icon_16x16.png"),
    ("icp5", "icon_16x16@2x.png"),
    ("icp6", "icon_32x32@2x.png"),
    ("ic07", "icon_128x128.png"),
    ("ic08", "icon_128x128@2x.png"),
    ("ic09", "icon_256x256@2x.png"),
    ("ic10", "icon_512x512@2x.png"),
]
chunks = []
for kind, filename in variants:
    image = (iconset / filename).read_bytes()
    chunks.append(kind.encode("ascii") + struct.pack(">I", len(image) + 8) + image)
payload = b"".join(chunks)
output.write_bytes(b"icns" + struct.pack(">I", len(payload) + 8) + payload)
PY
python3 - "$DEST" "$ROOT/.env" <<'PY'
import os, plistlib, stat, sys
from pathlib import Path
dest=Path(sys.argv[1])
# Generated apps can inherit the iCloud staging directory's hidden flag.
# Keep the installed app selectable in the macOS privacy permission picker.
os.chflags(dest, dest.stat().st_flags & ~stat.UF_HIDDEN)
info={'CFBundleIdentifier':'local.aide.computer-bridge','CFBundleName':'Aide Computer Bridge','CFBundleDisplayName':'Aide Computer Bridge','CFBundleExecutable':'AideComputerBridge','CFBundleIconFile':'AppIcon','CFBundlePackageType':'APPL','CFBundleVersion':'1','CFBundleShortVersionString':'1.0.0','LSMinimumSystemVersion':'14.0','NSHighResolutionCapable':True,'AideConfigurationFile':sys.argv[2]}
(dest/'Contents/Info.plist').write_bytes(plistlib.dumps(info))
PY
# Finder metadata from the iCloud build directory cannot be signed. Remove
# only these two metadata attributes from our generated bundle.
xattr -dr com.apple.FinderInfo "$DEST" 2>/dev/null || true
xattr -dr com.apple.ResourceFork "$DEST" 2>/dev/null || true
codesign --force --sign - --identifier local.aide.computer-bridge "$DEST"
echo "$DEST"
