#!/usr/bin/env bash
# Native LAN sockets; private mailbox + loopback Docker mapping, never host networking.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:-start}"
case "$MODE" in start|stop) ;; *) echo 'Usage: message-lan-control.sh {start|stop}' >&2; exit 2;; esac
DIR="$ROOT/.aide-data/storage/data/messages/lan-bridge"
NATIVE="$ROOT/.aide-data/message-lan"
case "$(uname -s)" in Darwin) target_os=darwin;; Linux) target_os=linux;; *) echo 'Use aide-launcher.exe on Windows.' >&2; exit 2;; esac
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
if command -v shasum >/dev/null 2>&1; then ACTUAL="$(shasum -a 256 "$TMP/$NAME" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then ACTUAL="$(sha256sum "$TMP/$NAME" | awk '{print $1}')"
else echo 'Missing shasum or sha256sum; cannot verify LAN helper.' >&2; exit 1; fi
[[ "$EXPECTED" =~ ^[a-f0-9]{64}$ && "$EXPECTED" == "$ACTUAL" ]] || { echo 'LAN helper checksum mismatch.' >&2; exit 1; }
chmod 700 "$TMP/$NAME"
# Detect optional remote-access support before stopping the current bridge or
# replacing its cached executable. Older images can still serve messaging.
REMOTE_TARGET=''
REMOTE_WARNING=''
HELPER_CAPABILITIES="$("$TMP/$NAME" -capabilities 2>/dev/null || true)"
HELPER_PROTOCOLS="$(python3 -c 'import json,sys; d=json.load(sys.stdin); c=d.get("capabilities",{}); print(int(d.get("protocol",0)), int(c.get("remoteAccess",0)))' <<< "$HELPER_CAPABILITIES" 2>/dev/null || true)"
HELPER_PROTOCOL=0
HELPER_REMOTE_PROTOCOL=0
read -r HELPER_PROTOCOL HELPER_REMOTE_PROTOCOL <<< "$HELPER_PROTOCOLS"
if [[ "$HELPER_PROTOCOL" != 1 ]]; then
  HELP="$("$TMP/$NAME" -h 2>&1 || true)"
  [[ "$HELP" == *"-remote-target"* ]] && HELPER_REMOTE_PROTOCOL=1 || HELPER_REMOTE_PROTOCOL=0
fi
SERVER_HEALTH="$("$DOCKER_BIN" compose exec -T aide curl -fsSk --connect-timeout 2 --max-time 3 https://127.0.0.1:8080/healthz 2>/dev/null || true)"
SERVER_REMOTE_PROTOCOL="$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(int(d.get("capabilities",{}).get("remoteAccessProtocol",0)))' <<< "$SERVER_HEALTH" 2>/dev/null || true)"
if [[ "$HELPER_REMOTE_PROTOCOL" =~ ^[1-9][0-9]*$ && "$SERVER_REMOTE_PROTOCOL" =~ ^[1-9][0-9]*$ && "$HELPER_REMOTE_PROTOCOL" -le "$SERVER_REMOTE_PROTOCOL" ]]; then
  if REMOTE_BINDING="$("$DOCKER_BIN" compose port aide 45842 2>/dev/null)" && [[ "$REMOTE_BINDING" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]]; then
    REMOTE_TARGET="127.0.0.1:${BASH_REMATCH[1]}"
  else
    REMOTE_WARNING='局域网远程访问不可用：运行环境缺少 45842 回环端口映射；聊天消息桥仍可用，请更新并重启 Aide。'
  fi
elif [[ "$HELPER_REMOTE_PROTOCOL" =~ ^[1-9][0-9]*$ ]]; then
  REMOTE_WARNING='局域网远程访问不可用：消息桥与服务端协议版本不兼容；聊天消息桥仍可用，请更新并重启 Aide。'
else
  REMOTE_WARNING='局域网远程访问不可用：当前消息桥不支持远程访问；聊天消息桥仍可用，请更新并重启 Aide。'
fi
[[ ! -x "$BIN" ]] || "$BIN" -stop -dir "$DIR"
mv "$TMP/$NAME" "$BIN"
if [[ -n "$REMOTE_WARNING" ]]; then
  echo "$REMOTE_WARNING" >&2
  printf '%s\n' "$REMOTE_WARNING" >> "$NATIVE/bridge.log"
fi
# A separate session survives the launching Terminal/tool process group.
python3 - "$BIN" "$DIR" "$TARGET" "$CAPTURE_TARGET" "$REMOTE_TARGET" "$NATIVE/bridge.log" <<'PYTHON'
import subprocess,sys
binary,directory,target,capture,remote,log=sys.argv[1:]
with open(log,'ab') as out:
    args=[binary,'-dir',directory,'-target',target,'-capture-target',capture]
    if remote:args.extend(['-remote-target',remote])
    subprocess.Popen(args,stdin=subprocess.DEVNULL,stdout=out,stderr=out,start_new_session=True,close_fds=True)
PYTHON
for attempt in {1..30}; do
  if "$BIN" -status -dir "$DIR" >/dev/null 2>&1; then echo '消息局域网桥已启动（不修改代理或防火墙）。'; exit 0; fi
  sleep .1
done
echo "LAN bridge not ready; see $NATIVE/bridge.log" >&2
exit 1
