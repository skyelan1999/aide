#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${AIDE_NATIVE_BRIDGE_APP:-${HOME}/Library/Application Support/Aide/Aide Computer Bridge.app}"
mkdir -p "$DEST/Contents/MacOS"
xcrun swiftc -swift-version 5 -module-cache-path "$ROOT/.cache/swift-modules" -target "$(uname -m)-apple-macos14.0" -O "$ROOT/scripts/native-computer-bridge.swift" -o "$DEST/Contents/MacOS/AideComputerBridge"
python3 - "$DEST" "$ROOT/.env" <<'PY'
import plistlib, sys
from pathlib import Path
dest=Path(sys.argv[1])
info={'CFBundleIdentifier':'local.aide.computer-bridge','CFBundleName':'Aide Computer Bridge','CFBundleDisplayName':'Aide Computer Bridge','CFBundleExecutable':'AideComputerBridge','CFBundlePackageType':'APPL','CFBundleVersion':'1','CFBundleShortVersionString':'1.0.0','LSMinimumSystemVersion':'14.0','NSHighResolutionCapable':True,'AideConfigurationFile':sys.argv[2]}
(dest/'Contents/Info.plist').write_bytes(plistlib.dumps(info))
PY
# Finder metadata from the iCloud build directory cannot be signed. Remove
# only these two metadata attributes from our generated bundle.
xattr -dr com.apple.FinderInfo "$DEST" 2>/dev/null || true
xattr -dr com.apple.ResourceFork "$DEST" 2>/dev/null || true
codesign --force --sign - --identifier local.aide.computer-bridge "$DEST"
echo "$DEST"
