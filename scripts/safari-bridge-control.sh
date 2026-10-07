#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:-start}"
PID_FILE="$ROOT/.cache/aide-safari-bridge.pid"
LOG_FILE="$ROOT/.cache/aide-safari-bridge.log"

read_token() {
  [[ -f .env ]] || return 1
  awk -F= '$1 == "AIDE_BROWSER_BRIDGE_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' .env
}

case "$MODE" in
  start)
    [[ "$(uname -s)" == Darwin ]] || { echo "Safari 桥接只在 macOS 上运行。" >&2; exit 2; }
    command -v node >/dev/null 2>&1 || { echo "未安装宿主机 Node.js，Safari 桥接未启动。" >&2; exit 2; }
    TOKEN="$(read_token || true)"
    [[ ${#TOKEN} -ge 32 ]] || { echo "AIDE_BROWSER_BRIDGE_TOKEN 未在 .env 配置；请先运行 scripts/aide.sh start。" >&2; exit 2; }
    mkdir -p .cache
    if [[ -s "$PID_FILE" ]]; then
      OLD_PID="$(cat "$PID_FILE" 2>/dev/null || true)"
      if [[ "$OLD_PID" =~ ^[0-9]+$ ]] && kill -0 "$OLD_PID" 2>/dev/null && ps -p "$OLD_PID" -o command= | grep -Fq 'scripts/safari-bridge.js'; then
        echo "Safari 桥接已运行（PID ${OLD_PID}）。"
        exit 0
      fi
      rm -f "$PID_FILE"
    fi
    AIDE_BROWSER_BRIDGE_TOKEN="$TOKEN" nohup node scripts/safari-bridge.js >>"$LOG_FILE" 2>&1 </dev/null &
    echo $! > "$PID_FILE"
    for _ in {1..30}; do
      if TOKEN="$TOKEN" node -e 'fetch("http://127.0.0.1:17777/healthz",{headers:{authorization:"Bearer "+process.env.TOKEN}}).then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))' 2>/dev/null; then
        echo "Safari 桥接已启动（Docker host gateway:17777）；Safari WebDriver 需在 Safari 开发者设置中启用。"
        exit 0
      fi
      sleep 0.2
    done
    echo "Safari 桥接启动失败，检查 ${LOG_FILE}。" >&2
    exit 1
    ;;
  stop)
    if [[ -s "$PID_FILE" ]]; then
      PID="$(cat "$PID_FILE" 2>/dev/null || true)"
      if [[ "$PID" =~ ^[0-9]+$ ]] && kill -0 "$PID" 2>/dev/null && ps -p "$PID" -o command= | grep -Fq 'scripts/safari-bridge.js'; then
        kill "$PID"
      fi
      rm -f "$PID_FILE"
    fi
    ;;
  *) echo "用法：$0 {start|stop}" >&2; exit 2 ;;
esac
