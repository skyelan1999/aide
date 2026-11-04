#!/usr/bin/env bash
# Build an installable macOS app/package whose runtime does not require Docker.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[[ "$(uname -s)" == Darwin ]] || { echo 'Native macOS packaging must run on macOS.' >&2; exit 2; }
command -v swiftc >/dev/null && command -v codesign >/dev/null && command -v productbuild >/dev/null && command -v ditto >/dev/null || { echo 'Xcode Command Line Tools are required.' >&2; exit 1; }
OUT="${AIDE_NATIVE_PACKAGE_OUT:-$ROOT/dist/native-macos}"
mkdir -p "$OUT"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-package.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$STAGE"' EXIT
APP="$STAGE/Aide.app"
RES="$APP/Contents/Resources"
mkdir -p "$APP/Contents/MacOS" "$RES/runtime/bin" "$RES/runtime/lib/node_modules" "$RES/runtime/python"
SERVER="${AIDE_NATIVE_SERVER_BINARY:-$ROOT/build/native/aide}"
if [[ ! -x "$SERVER" ]]; then
  command -v go >/dev/null 2>&1 || { echo 'Go is needed to build the native Aide service; set AIDE_NATIVE_SERVER_BINARY to a matching macOS binary.' >&2; exit 1; }
  scripts/build-native-runtime.sh
fi
[[ "$(file -b "$SERVER")" == *Mach-O* ]] || { echo 'AIDE_NATIVE_SERVER_BINARY is not a macOS executable.' >&2; exit 1; }
HOST_ARCH="$(uname -m)"
SERVER_ARCHS="$(lipo -archs "$SERVER" 2>/dev/null || true)"
[[ " $SERVER_ARCHS " == *" $HOST_ARCH "* ]] || { echo "Aide service architecture ($SERVER_ARCHS) does not include this Mac ($HOST_ARCH)." >&2; exit 1; }
NODE="${AIDE_NODE_BIN:-$(command -v node || true)}"
[[ -x "$NODE" ]] || { echo 'Node.js 24 is required to package plugin runtime support.' >&2; exit 1; }
NODE_MAJOR="$("$NODE" -p 'process.versions.node.split(".")[0]')"
[[ "$NODE_MAJOR" == 24 ]] || { echo "Node.js 24 is required; found major $NODE_MAJOR." >&2; exit 1; }
PYTHON_RUNTIME="${AIDE_PYTHON_RUNTIME:-$ROOT/build/native/python}"
if [[ ! -x "$PYTHON_RUNTIME/bin/python3.12" ]]; then
  AIDE_NATIVE_PYTHON_OUT="$ROOT/build/native/python" scripts/build-native-python-macos.sh
  PYTHON_RUNTIME="$ROOT/build/native/python"
fi
PYTHONPATH="$PYTHON_RUNTIME/lib/python3.12/site-packages" "$PYTHON_RUNTIME/bin/python3.12" -c 'import docx,openpyxl,pptx,ezdxf,pypdf; assert pypdf.__version__ == "6.19.0"' || { echo 'Bundled Python runtime is missing the pinned Office modules.' >&2; exit 1; }
cp "$SERVER" "$RES/aide-server"
chmod 755 "$RES/aide-server"
cp "$NODE" "$RES/runtime/bin/node"
chmod 755 "$RES/runtime/bin/node"
NPM_CLI="${AIDE_NPM_BIN:-$(command -v npm || true)}"
NPM_GLOBAL_ROOT=""
if [[ -x "$NPM_CLI" ]]; then NPM_GLOBAL_ROOT="$("$NPM_CLI" root -g 2>/dev/null || true)"; fi
NPM_ROOT="$NPM_GLOBAL_ROOT/npm"
if [[ -n "$NPM_GLOBAL_ROOT" && -f "$NPM_ROOT/bin/npm-cli.js" ]]; then
  cp -R "$NPM_ROOT" "$RES/runtime/lib/node_modules/npm"
  ln -s ../lib/node_modules/npm/bin/npm-cli.js "$RES/runtime/bin/npm"
  ln -s ../lib/node_modules/npm/bin/npx-cli.js "$RES/runtime/bin/npx"
fi
ditto "$PYTHON_RUNTIME" "$RES/runtime/python"
cp -R scripts/office "$RES/office-scripts"
VERSION="$(python3 -c 'import re,sys; s=open(sys.argv[1]).read(); m=re.search(r"(\d+\.\d+\.\d+\.\d+) RC(\d+)",s); print((m.group(1)+"."+m.group(2)) if m else "0.1.0")' version.md)"
python3 - "$APP/Contents/Info.plist" "$VERSION" <<'PY'
import plistlib,sys
path,version=sys.argv[1:]
value={"CFBundleIdentifier":"local.aide.desktop.native","CFBundleName":"Aide","CFBundleDisplayName":"Aide","CFBundleExecutable":"AideDesktop","CFBundlePackageType":"APPL","CFBundleVersion":version,"CFBundleShortVersionString":".".join(version.split('.')[:4]),"LSMinimumSystemVersion":"14.0","LSApplicationCategoryType":"public.app-category.developer-tools","NSHighResolutionCapable":True,"AideNativeRuntime":True,"NSMicrophoneUsageDescription":"Aide 使用麦克风进行你主动开启的语音输入和通话。","NSSpeechRecognitionUsageDescription":"Aide 仅在你主动开启语音输入时请求语音识别。","NSCameraUsageDescription":"Aide 仅在你主动使用摄像头功能时请求访问。","NSLocalNetworkUsageDescription":"Aide 连接本机工作台服务。"}
with open(path,'wb') as f: plistlib.dump(value,f)
PY
swiftc -swift-version 5 -O -target "$(uname -m)-apple-macos14.0" native/macos-desktop/main.swift native/macos-desktop/biometrics.swift -o "$APP/Contents/MacOS/AideDesktop"
python3 - "$STAGE/entitlements.plist" <<'PY'
import plistlib,sys
with open(sys.argv[1],'wb') as f: plistlib.dump({'com.apple.security.device.audio-input': True,'com.apple.security.device.camera': True},f)
PY
codesign --force --sign - --identifier local.aide.desktop.native --entitlements "$STAGE/entitlements.plist" "$APP"
codesign --verify --deep --strict "$APP"
[[ ! -e "$OUT/Aide-Native-macOS.pkg" && ! -e "$OUT/Aide-Native-macOS.zip" ]] || { echo 'Native package output already exists; choose another AIDE_NATIVE_PACKAGE_OUT.' >&2; exit 1; }
productbuild --component "$APP" /Applications "$OUT/Aide-Native-macOS.pkg" >/dev/null
ditto -c -k --sequesterRsrc --keepParent "$APP" "$OUT/Aide-Native-macOS.zip"
if [[ "${AIDE_BUILD_DMG:-0}" == 1 ]]; then
  hdiutil create -volname Aide -srcfolder "$APP" -format UDZO "$OUT/Aide-Native-macOS.dmg"
fi
printf 'PKG: %s\nZIP: %s\n' "$OUT/Aide-Native-macOS.pkg" "$OUT/Aide-Native-macOS.zip"
