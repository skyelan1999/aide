#!/usr/bin/env bash
# Live smoke test: launches the native executable with Docker hidden from PATH.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${AIDE_NATIVE_BIN:-$ROOT/build/native/aide}"
[[ -x "$BIN" ]] || { echo "Native Aide binary not found: $BIN" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo 'curl is required for the live smoke test.' >&2; exit 1; }
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-live.XXXXXX")"
PID=''
cleanup() {
  if [[ -n "$PID" ]]; then kill -TERM "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; fi
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT
PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
PATH="/usr/local/bin:/usr/bin:/bin" HOME="$TEST_ROOT/home" \
  AIDE_WORKSPACE="$TEST_ROOT/workspace" AIDE_CONTEXT="$TEST_ROOT/context" AIDE_DATA="$TEST_ROOT/data" \
  AIDE_ADDR="127.0.0.1:$PORT" AIDE_LAN_ADDR="127.0.0.1:0" \
  "$BIN" >"$TEST_ROOT/aide.log" 2>&1 &
PID=$!
ready=0
for _ in {1..45}; do
  if curl -ksf --max-time 2 "https://127.0.0.1:$PORT/healthz" >"$TEST_ROOT/health.json"; then ready=1; break; fi
  sleep 1
done
if (( ! ready )); then cat "$TEST_ROOT/aide.log" >&2; exit 1; fi
python3 - "$TEST_ROOT/health.json" <<'PY'
import json,sys
health=json.load(open(sys.argv[1]))
assert health.get('service') == 'aide' and health.get('status') == 'ok', health
assert health.get('integrity') == 'ok', health
print('healthz:',json.dumps(health,ensure_ascii=False))
PY
status="$(curl -ks -o /dev/null -w '%{http_code}' --max-time 3 "https://127.0.0.1:$PORT/")"
[[ "$status" == 200 ]] || { echo "Workbench returned HTTP $status" >&2; exit 1; }
printf 'workbench: HTTP %s; runtime launched with Docker absent from PATH\n' "$status"
