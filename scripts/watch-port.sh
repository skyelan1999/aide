#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
DOCKER_BIN="$(command -v docker || true)"
[[ -n "$DOCKER_BIN" ]] || exit 0
LOCK=.aide-port-watcher.lock
mkdir "$LOCK" 2>/dev/null || exit 0
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT INT TERM

while "$DOCKER_BIN" compose ps --status running --services 2>/dev/null | grep -qx aide; do
  desired="$("$DOCKER_BIN" compose exec -T aide sh -c 'cat /data/config/host-port 2>/dev/null || true' 2>/dev/null | tr -d '\r\n')"
  if [[ "$desired" =~ ^[0-9]{1,5}$ ]] && (( desired >= 1 && desired <= 65535 )); then
    current="$(awk -F= '$1 == "AIDE_PORT" { gsub(/[[:space:]]/, "", $2); print $2; exit }' .env 2>/dev/null || true)"
    if [[ "$desired" != "$current" ]]; then
      busy=0
      if command -v lsof >/dev/null 2>&1; then
        lsof -nP -iTCP:"$desired" -sTCP:LISTEN -t >/dev/null 2>&1 && busy=1 || true
      elif command -v ss >/dev/null 2>&1; then
        [[ -n "$(ss -H -ltn "sport = :$desired" 2>/dev/null)" ]] && busy=1 || true
      fi
      if (( busy == 0 )); then
        if grep -q '^AIDE_PORT=' .env; then
          sed -i.bak "s/^AIDE_PORT=.*/AIDE_PORT=$desired/" .env && rm -f .env.bak
        else
          printf '\nAIDE_PORT=%s\n' "$desired" >> .env
        fi
        "$DOCKER_BIN" compose up -d --no-build --pull never
        # The settings page polls the requested listener and replaces its own
        # URL when the service is ready, keeping one browser tab and its token.
      fi
    fi
  fi
  sleep 2
done
