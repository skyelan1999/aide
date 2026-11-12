#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:-start}"
PID_FILE="$ROOT/.cache/aide-computer-bridge.pid"
LOG_FILE="$ROOT/.cache/aide-computer-bridge.log"
if [[ "${AIDE_BRIDGE_LOCKED:-}" != "$PID_FILE" ]]; then
  exec python3 "$ROOT/scripts/bridge-lifecycle-lock.py" "$PID_FILE" "$0" "$@"
fi
NATIVE_APP="${AIDE_NATIVE_BRIDGE_APP:-${HOME}/Library/Application Support/Aide/Aide Computer Bridge.app}"
read_token() { [[ -f .env ]] || return 1; awk -F= '$1 == "AIDE_COMPUTER_BRIDGE_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' .env; }
case "$MODE" in
  start)
    command -v node >/dev/null 2>&1 || { echo "未安装宿主机 Node.js，电脑桥接未启动。" >&2; exit 2; }
    TOKEN="$(read_token || true)"
    [[ ${#TOKEN} -ge 32 ]] || { echo "AIDE_COMPUTER_BRIDGE_TOKEN 未在 .env 配置；请先运行 scripts/aide.sh start。" >&2; exit 2; }
    if [[ "$(uname -s)" == Linux ]]; then
      [[ "${XDG_SESSION_TYPE:-}" == x11 ]] || { echo "Linux 电脑桥接目前需要 X11 会话；Wayland 原生窗口暂不支持。" >&2; exit 2; }
      command -v xdotool >/dev/null 2>&1 || { echo "Linux 电脑桥接需要 xdotool。" >&2; exit 2; }
      command -v import >/dev/null 2>&1 || { echo "Linux 电脑截图需要 ImageMagick import。" >&2; exit 2; }
      mkdir -p .cache
      if [[ -s "$PID_FILE" ]]; then
        OLD_PID="$(cat "$PID_FILE" 2>/dev/null || true)"
        if [[ "$OLD_PID" =~ ^[0-9]+$ ]] && kill -0 "$OLD_PID" 2>/dev/null && ps -p "$OLD_PID" -o command= | grep -Fq 'scripts/computer-bridge-linux.js'; then echo "Linux 电脑桥接已运行（PID ${OLD_PID}）。"; exit 0; fi
        rm -f "$PID_FILE"
      fi
      AIDE_COMPUTER_BRIDGE_TOKEN="$TOKEN" nohup node scripts/computer-bridge-linux.js >>"$LOG_FILE" 2>&1 </dev/null &
      CHILD_PID=$!
    echo "$CHILD_PID" > "$PID_FILE"
      for _ in {1..30}; do
        if TOKEN="$TOKEN" EXPECTED_PID="$CHILD_PID" node -e 'fetch("http://127.0.0.1:17778/healthz",{headers:{authorization:"Bearer "+process.env.TOKEN}}).then(r=>r.json()).then(x=>process.exit(x.ok&&x.pid===Number(process.env.EXPECTED_PID)&&x.service==="aide-linux-computer-bridge"?0:1)).catch(()=>process.exit(1))' 2>/dev/null; then
          echo "Linux 电脑桥接已启动（X11）；点击、输入和按键仍需 Aide 逐次确认。"
          exit 0
        fi
        sleep 0.2
      done
    kill "$CHILD_PID" 2>/dev/null || true
    rm -f "$PID_FILE"
      echo "Linux 电脑桥接启动失败，检查 ${LOG_FILE}。" >&2; exit 1
    fi
    [[ "$(uname -s)" == Darwin ]] || { echo "当前系统没有配置电脑桥接。" >&2; exit 2; }
    mkdir -p .cache
    if [[ -d "$NATIVE_APP" ]]; then
      /usr/bin/open -g "$NATIVE_APP"
      for _ in {1..30}; do
        if TOKEN="$TOKEN" node -e 'fetch("http://127.0.0.1:17778/healthz",{headers:{authorization:"Bearer "+process.env.TOKEN}}).then(async r=>{const x=await r.json();process.exit(r.ok&&x.service==="aide-native-computer-bridge"?0:1)}).catch(()=>process.exit(1))' 2>/dev/null; then
          echo "Aide 电脑控制服务已就绪；请在 Aide 设置 → 电脑控制中查看并申请系统授权。"; exit 0
        fi
        sleep 0.2
      done
      echo "电脑控制服务启动失败；请在 Aide 设置中刷新授权状态，并检查桥接日志。" >&2; exit 1
    fi
    if [[ -s "$PID_FILE" ]]; then
      OLD_PID="$(cat "$PID_FILE" 2>/dev/null || true)"
      if [[ "$OLD_PID" =~ ^[0-9]+$ ]] && kill -0 "$OLD_PID" 2>/dev/null && ps -p "$OLD_PID" -o command= | grep -Fq 'scripts/computer-bridge.js'; then echo "电脑桥接已运行（PID ${OLD_PID}）。"; exit 0; fi
      rm -f "$PID_FILE"
    fi
    AIDE_COMPUTER_BRIDGE_TOKEN="$TOKEN" nohup node scripts/computer-bridge.js >>"$LOG_FILE" 2>&1 </dev/null &
    CHILD_PID=$!
    echo "$CHILD_PID" > "$PID_FILE"
    for _ in {1..30}; do
      if TOKEN="$TOKEN" EXPECTED_PID="$CHILD_PID" node -e 'fetch("http://127.0.0.1:17778/healthz",{headers:{authorization:"Bearer "+process.env.TOKEN}}).then(async r=>{const x=await r.json();process.exit(r.ok&&x.pid===Number(process.env.EXPECTED_PID)?0:1)}).catch(()=>process.exit(1))' 2>/dev/null; then
        echo "电脑桥接已启动（Docker host gateway:17778）；屏幕录制和辅助功能权限需在 macOS 首次授权。"
        exit 0
      fi
      sleep 0.2
    done
    kill "$CHILD_PID" 2>/dev/null || true
    rm -f "$PID_FILE"
    echo "电脑桥接启动失败，检查 ${LOG_FILE}。" >&2; exit 1
    ;;
  stop)
    if [[ "$(uname -s)" == Linux && -s "$PID_FILE" ]]; then
      PID="$(cat "$PID_FILE" 2>/dev/null || true)"
      if [[ "$PID" =~ ^[0-9]+$ ]] && kill -0 "$PID" 2>/dev/null && ps -p "$PID" -o command= | grep -Fq 'scripts/computer-bridge-linux.js'; then kill "$PID"; fi
      rm -f "$PID_FILE"
    fi
    if [[ -d "$NATIVE_APP" ]]; then
      for NATIVE_PID in $(pgrep -f "$NATIVE_APP/Contents/MacOS/AideComputerBridge" || true); do
        if ps -p "$NATIVE_PID" -o command= | grep -Fq "$NATIVE_APP/Contents/MacOS/AideComputerBridge"; then kill "$NATIVE_PID"; fi
      done
    fi
    if [[ -s "$PID_FILE" ]]; then PID="$(cat "$PID_FILE" 2>/dev/null || true)"; if [[ "$PID" =~ ^[0-9]+$ ]] && kill -0 "$PID" 2>/dev/null && ps -p "$PID" -o command= | grep -Fq 'scripts/computer-bridge.js'; then kill "$PID"; fi; rm -f "$PID_FILE"; fi
    ;;
  *) echo "用法：$0 {start|stop}" >&2; exit 2 ;;
esac
