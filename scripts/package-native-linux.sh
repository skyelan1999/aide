#!/usr/bin/env bash
# Build an installable Debian package with a bundled Aide, Node, Python and Office runtime.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[[ "$(uname -s)" == Linux ]] || { echo 'Native Linux packaging must run on Linux.' >&2; exit 2; }
command -v dpkg-deb >/dev/null && command -v dpkg >/dev/null && command -v file >/dev/null || { echo 'dpkg and file are required to build the Debian package.' >&2; exit 1; }
OUT="${AIDE_NATIVE_PACKAGE_OUT:-$ROOT/dist/native-linux}"
mkdir -p "$OUT"
[[ ! -e "$OUT/aide-native-experiment.deb" ]] || { echo 'Package output already exists; choose another AIDE_NATIVE_PACKAGE_OUT.' >&2; exit 1; }
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-linux.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$STAGE"' EXIT
SERVER="${AIDE_NATIVE_SERVER_BINARY:-$ROOT/build/native/aide}"
if [[ ! -x "$SERVER" ]]; then
  command -v go >/dev/null || { echo 'Go is required to build the Linux service; set AIDE_NATIVE_SERVER_BINARY to a Linux binary.' >&2; exit 1; }
  scripts/build-native-runtime.sh
fi
[[ "$(file -b "$SERVER")" == *ELF* ]] || { echo 'AIDE_NATIVE_SERVER_BINARY is not a Linux executable.' >&2; exit 1; }
case "$(uname -m):$(file -b "$SERVER")" in
  aarch64:*ARM\ aarch64*) ;;
  x86_64:*x86-64*) ;;
  *) echo "Service binary architecture does not match this Linux host: $(file -b "$SERVER")" >&2; exit 1 ;;
esac
PYTHON_RUNTIME="${AIDE_PYTHON_RUNTIME:-$ROOT/build/native/python}"
if [[ ! -x "$PYTHON_RUNTIME/bin/python3.12" ]]; then
  AIDE_NATIVE_PYTHON_OUT="$PYTHON_RUNTIME" scripts/build-native-python-linux.sh
fi
NODE_RUNTIME="${AIDE_NODE_RUNTIME:-$ROOT/build/native/node}"
if [[ ! -x "$NODE_RUNTIME/bin/node" ]]; then
  AIDE_NATIVE_NODE_OUT="$NODE_RUNTIME" scripts/build-native-node-linux.sh
fi
PYTHONPATH="$PYTHON_RUNTIME/lib/python3.12/site-packages" "$PYTHON_RUNTIME/bin/python3.12" -c 'import docx,openpyxl,pptx,ezdxf,pypdf; assert pypdf.__version__ == "6.19.0"'
[[ "$("$NODE_RUNTIME/bin/node" -p 'process.versions.node.split(".")[0]')" == 24 ]] || { echo 'Bundled Node runtime must be v24.' >&2; exit 1; }
PKG="$STAGE/package"
RES="$PKG/usr/lib/aide-native"
mkdir -p "$RES/runtime" "$RES/office-scripts" "$PKG/usr/bin" "$PKG/usr/share/applications" "$PKG/DEBIAN"
install -m 0755 "$SERVER" "$RES/aide"
cp -a "$NODE_RUNTIME" "$RES/runtime/node"
cp -a "$PYTHON_RUNTIME" "$RES/runtime/python"
cp -a scripts/office/. "$RES/office-scripts/"
cat > "$RES/native-env.sh" <<'SH'
#!/bin/sh
set -eu
ROOT="/usr/lib/aide-native"
HOME="${HOME:?Aide must run as a normal user, not without a home directory}"
DATA="${AIDE_DATA:-${XDG_DATA_HOME:-$HOME/.local/share}/aide/native}"
WORKSPACE="${AIDE_WORKSPACE:-$HOME/Documents/Aide/Workspace}"
CONTEXT="${AIDE_CONTEXT:-$HOME/Documents/Aide/References}"
mkdir -p "$DATA" "$WORKSPACE" "$CONTEXT"
export HOME AIDE_DATA="$DATA" AIDE_WORKSPACE="$WORKSPACE" AIDE_CONTEXT="$CONTEXT"
export AIDE_ADDR="${AIDE_ADDR:-127.0.0.1:8097}" AIDE_LAN_ADDR="${AIDE_LAN_ADDR:-127.0.0.1:0}"
export AIDE_HOST_LOCAL="$HOME" AIDE_RUNTIME_MODE=native
export AIDE_NATIVE_LOCAL_HTTP=1
export AIDE_OFFICE_SCRIPTS="$ROOT/office-scripts"
export PATH="$ROOT/runtime/node/bin:$ROOT/runtime/python/bin:$PATH"
export PYTHONPATH="$ROOT/runtime/python/lib/python3.12/site-packages"
SH
cat > "$PKG/usr/bin/aide-native" <<'SH'
#!/bin/sh
set -eu
. /usr/lib/aide-native/native-env.sh
exec "$ROOT/aide"
SH
cat > "$PKG/usr/bin/aide-native-ui" <<'SH'
#!/bin/sh
set -eu
. /usr/lib/aide-native/native-env.sh
case "$AIDE_ADDR" in
  127.0.0.1:*|localhost:*) ;;
  *) echo 'The native desktop launcher requires AIDE_ADDR to use loopback.' >&2; exit 1 ;;
esac
PORT="${AIDE_ADDR##*:}"
BASE="http://127.0.0.1:$PORT"
LOG="$DATA/logs/service.log"
PIDFILE="$DATA/service.pid"
mkdir -p "$DATA/logs"
if "$ROOT/runtime/python/bin/python3.12" -I -c 'import socket,sys; s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); r=s.connect_ex(("127.0.0.1", int(sys.argv[1]))); s.close(); raise SystemExit(0 if r == 0 else 1)' "$PORT"; then
  # Reuse only our own already-running instance; never attach to another app
  # that happens to answer on Aide's preferred port.
  TOKEN="$(cat "$DATA/auth/access-token" 2>/dev/null || true)"
  if [ -z "$TOKEN" ] || ! curl -fsS --max-time 2 "$BASE/healthz" >/dev/null 2>&1 || ! curl -fsS --max-time 2 -H "Authorization: Bearer $TOKEN" "$BASE/api/config" >/dev/null 2>&1; then
    PORT="$("$ROOT/runtime/python/bin/python3.12" -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
    AIDE_ADDR="127.0.0.1:$PORT"
    export AIDE_ADDR
    BASE="http://127.0.0.1:$PORT"
  fi
fi
if ! curl -fsS --max-time 2 "$BASE/healthz" >/dev/null 2>&1; then
  nohup /usr/bin/aide-native >"$LOG" 2>&1 </dev/null &
  echo "$!" >"$PIDFILE"
  ready=0
  for _ in $(seq 1 45); do
    if curl -fsS --max-time 2 "$BASE/healthz" >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
  done
  if [ "$ready" -ne 1 ]; then echo "Aide failed to start. See $LOG" >&2; exit 1; fi
fi
TOKEN="$(cat "$DATA/auth/access-token" 2>/dev/null || cat "$DATA/access-token" 2>/dev/null || true)"
case "$TOKEN" in ''|*[!A-Za-z0-9_.-]*) echo 'Aide access token is unavailable.' >&2; exit 1 ;; esac
xdg-open "http://localhost:$PORT/#token=$TOKEN"
SH
cat > "$PKG/usr/bin/aide-native-stop" <<'SH'
#!/bin/sh
set -eu
DATA="${AIDE_DATA:-${XDG_DATA_HOME:-${HOME:?}/.local/share}/aide/native}"
PIDFILE="$DATA/service.pid"
if [ -f "$PIDFILE" ]; then
  PID="$(cat "$PIDFILE")"
  case "$PID" in ''|*[!0-9]*) echo 'Invalid Aide PID file.' >&2; exit 1 ;; esac
  kill -TERM "$PID" 2>/dev/null || true
  rm -f "$PIDFILE"
fi
SH
chmod 0755 "$RES/native-env.sh" "$PKG/usr/bin/aide-native" "$PKG/usr/bin/aide-native-ui" "$PKG/usr/bin/aide-native-stop"
cat > "$PKG/usr/share/applications/aide-native-experiment.desktop" <<'DESKTOP'
[Desktop Entry]
Name=Aide Native Experiment
Comment=Start the Docker-free Aide workbench
Exec=/usr/bin/aide-native-ui
Terminal=false
Type=Application
Icon=utilities-terminal
Categories=Development;
DESKTOP
VERSION="$(python3 -c 'import re,sys; s=open(sys.argv[1]).read(); m=re.search(r"(\d+\.\d+\.\d+\.\d+) RC(\d+)",s); print((m[1]+"."+m[2]) if m else "0.1.0")' version.md)"
ARCH="$(dpkg --print-architecture)"
cat > "$PKG/DEBIAN/control" <<EOF
Package: aide-native-experiment
Version: $VERSION
Section: devel
Priority: optional
Architecture: $ARCH
Maintainer: Aide Experimental Build
Depends: libc6 (>= 2.34), libstdc++6, libgcc-s1, ca-certificates, curl, git, openssh-client, xdg-utils
Description: Experimental Docker-free Aide runtime
 Aide service and Node/Python runtime installed independently of Docker.
EOF
dpkg-deb --build --root-owner-group "$PKG" "$OUT/aide-native-experiment.deb" >/dev/null
printf 'DEB: %s\n' "$OUT/aide-native-experiment.deb"
