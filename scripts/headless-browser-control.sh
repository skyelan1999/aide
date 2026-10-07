#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:-start}"
PID_FILE="$ROOT/.cache/aide-headless-browser.pid"
LOG_FILE="$ROOT/.cache/aide-headless-browser.log"
case "$MODE" in
  install)
    npm ci --prefix scripts/browser-runtime --ignore-scripts --no-audit --no-fund
    PLAYWRIGHT_BROWSERS_PATH="${HOME}/Library/Caches/aide-browser-binaries" node scripts/browser-runtime/node_modules/playwright/cli.js install chromium --only-shell
    ;;
  start)
    [[ -f scripts/browser-runtime/node_modules/playwright/package.json ]] || { echo 'Run bash scripts/headless-browser-control.sh install first.' >&2; exit 2; }
    TOKEN="$(awk -F= '$1 == "AIDE_BROWSER_BRIDGE_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' .env)"
    [[ ${#TOKEN} -ge 32 ]] || { echo 'Browser bridge token is not configured.' >&2; exit 2; }
    mkdir -p .cache
    if [[ -s "$PID_FILE" ]]; then
      OLD_PID="$(cat "$PID_FILE")"
      if [[ "$OLD_PID" =~ ^[0-9]+$ ]] && kill -0 "$OLD_PID" 2>/dev/null && ps -p "$OLD_PID" -o command= | grep -Fq 'scripts/headless-browser-bridge.js'; then
        echo "Headless browser already running (PID ${OLD_PID})."; exit 0
      fi
    fi
    CHILD_PID="$(AIDE_BROWSER_BRIDGE_TOKEN="$TOKEN" node -e 'const fs=require("node:fs"); const fd=fs.openSync(process.argv[2],"a"); const child=require("node:child_process").spawn(process.execPath,[process.argv[1]],{detached:true,stdio:["ignore",fd,fd],env:process.env}); child.unref(); fs.closeSync(fd); process.stdout.write(String(child.pid));' scripts/headless-browser-bridge.js "$LOG_FILE")"
    echo "$CHILD_PID" > "$PID_FILE"
    for _ in {1..30}; do
      if TOKEN="$TOKEN" node -e 'fetch("http://127.0.0.1:17779/healthz",{headers:{authorization:"Bearer "+process.env.TOKEN}}).then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))' 2>/dev/null; then
        echo "Headless Chromium ready on 17779 (PID ${CHILD_PID})."; exit 0
      fi
      kill -0 "$CHILD_PID" 2>/dev/null || break
      sleep 0.2
    done
    kill "$CHILD_PID" 2>/dev/null || true
    rm -f "$PID_FILE"
    echo "Headless browser failed; check ${LOG_FILE}." >&2; exit 1
    ;;
  stop)
    if [[ -s "$PID_FILE" ]]; then
      PID="$(cat "$PID_FILE")"
      if [[ "$PID" =~ ^[0-9]+$ ]] && kill -0 "$PID" 2>/dev/null && ps -p "$PID" -o command= | grep -Fq 'scripts/headless-browser-bridge.js'; then kill "$PID"; fi
      rm -f "$PID_FILE"
    fi
    ;;
  *) echo "Usage: $0 {install|start|stop}" >&2; exit 2 ;;
esac
