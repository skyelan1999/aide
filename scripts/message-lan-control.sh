#!/usr/bin/env bash
# Native LAN sockets; private mailbox + loopback Docker mapping, never host networking.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:-start}"
case "$MODE" in start|stop) ;; *) echo 'Usage: message-lan-control.sh {start|stop}' >&2; exit 2;; esac
DIR="$ROOT/.aide-data/storage/data/messages/lan-bridge"
NATIVE="$ROOT/.aide-data/message-lan"
case "$(uname -s)" in Darwin) target_os=darwin;; Linux) target_os=linux;; *) echo 'Use message-lan-control.ps1 on Windows.' >&2; exit 2;; esac
case "$(uname -m)" in arm64|aarch64) target_arch=arm64;; x86_64|amd64) target_arch=amd64;; *) echo 'Unsupported LAN bridge architecture.' >&2; exit 2;; esac
NAME="aide-message-lan-$target_os-$target_arch"
BIN="$NATIVE/$NAME"
if [[ "$MODE" == stop ]]; then [[ ! -x "$BIN" ]] || "$BIN" -stop -dir "$DIR"; exit; fi
DOCKER_BIN="${DOCKER_BIN:-$(command -v docker || true)}"
[[ -n "$DOCKER_BIN" ]] || { echo 'Docker CLI not found.' >&2; exit 1; }
CID="$("$DOCKER_BIN" compose ps -q aide)"
[[ -n "$CID" && "$CID" != *$'\n'* ]] || { echo 'Cannot identify the Aide container.' >&2; exit 1; }
BINDING="$("$DOCKER_BIN" compose port aide 45840)"
[[ "$BINDING" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]] || { echo 'LAN bridge requires a loopback-only message port mapping. Restart using the updated Compose file.' >&2; exit 1; }
TARGET="127.0.0.1:${BASH_REMATCH[1]}"
CAPTURE_BINDING="$("$DOCKER_BIN" compose port aide 45841)"
[[ "$CAPTURE_BINDING" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]] || { echo "Missing loopback collector mapping; restart Aide." >&2; exit 1; }
CAPTURE_TARGET="127.0.0.1:${BASH_REMATCH[1]}"
IMAGE="$("$DOCKER_BIN" inspect "$CID" --format '{{.Image}}')"
[[ "$IMAGE" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo 'Invalid running image identity.' >&2; exit 1; }
mkdir -p "$NATIVE" "$DIR"
chmod 700 "$NATIVE"
# Extract from a fresh immutable image container, not a mutable running filesystem.
TMP="$(mktemp -d "$NATIVE/extract.XXXXXX")"
SOURCE=''
cleanup() { [[ -z "$SOURCE" ]] || "$DOCKER_BIN" rm "$SOURCE" >/dev/null 2>&1 || true; rm -rf "$TMP"; }
trap cleanup EXIT
SOURCE="$("$DOCKER_BIN" create --network none --entrypoint /bin/true "$IMAGE")"
"$DOCKER_BIN" cp "$SOURCE:/opt/aide/message-lan/$NAME" "$TMP/$NAME"
"$DOCKER_BIN" cp "$SOURCE:/opt/aide/message-lan/SHA256SUMS" "$TMP/SHA256SUMS"
EXPECTED="$(awk -v name="$NAME" '$2==name {print $1}' "$TMP/SHA256SUMS")"
ACTUAL="$(shasum -a 256 "$TMP/$NAME" | awk '{print $1}')"
[[ "$EXPECTED" =~ ^[a-f0-9]{64}$ && "$EXPECTED" == "$ACTUAL" ]] || { echo 'LAN helper checksum mismatch.' >&2; exit 1; }
[[ ! -x "$BIN" ]] || "$BIN" -stop -dir "$DIR"
chmod 700 "$TMP/$NAME"
mv "$TMP/$NAME" "$BIN"
# A separate session survives the launching Terminal/tool process group.
python3 - "$BIN" "$DIR" "$TARGET" "$CAPTURE_TARGET" "$NATIVE/bridge.log" <<'PYTHON'
import subprocess,sys
binary,directory,target,capture,log=sys.argv[1:]
with open(log,'ab') as out:
    subprocess.Popen([binary,'-dir',directory,'-target',target,'-capture-target',capture],stdin=subprocess.DEVNULL,stdout=out,stderr=out,start_new_session=True,close_fds=True)
PYTHON
for attempt in {1..30}; do
  if "$BIN" -status -dir "$DIR" >/dev/null 2>&1; then echo '消息局域网桥已启动（不修改代理或防火墙）。'; exit 0; fi
  sleep .1
done
echo "LAN bridge not ready; see $NATIVE/bridge.log" >&2
exit 1
