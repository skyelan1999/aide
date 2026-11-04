#!/usr/bin/env bash
set -euo pipefail

BRIDGE_APP="${1:?usage: test_macos_native_bridge_launch.sh /path/to/Aide\ Computer\ Bridge.app}"
[[ -d "$BRIDGE_APP" ]] || { echo "Bridge app not found: $BRIDGE_APP" >&2; exit 2; }
command -v swift >/dev/null
command -v curl >/dev/null
command -v lsof >/dev/null

TMP="$(mktemp -d "${TMPDIR:-/tmp}/aide-bridge-launch.XXXXXX")"
BRIDGE_PID=""
cleanup() {
  if [[ -n "$BRIDGE_PID" ]]; then kill "$BRIDGE_PID" 2>/dev/null || true; fi
  python3 - "$TMP" <<'PY'
import shutil,sys
shutil.rmtree(sys.argv[1], ignore_errors=True)
PY
}
trap cleanup EXIT

TOKEN="$(openssl rand -hex 32)"
TOKEN_FILE="$TMP/token"
printf '%s' "$TOKEN" > "$TOKEN_FILE"
chmod 600 "$TOKEN_FILE"
PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
cat > "$TMP/launch.swift" <<'SWIFT'
import AppKit
import Foundation
let app = URL(fileURLWithPath: CommandLine.arguments[1])
let config = NSWorkspace.OpenConfiguration()
config.activates = false
config.environment = [
    "AIDE_COMPUTER_BRIDGE_TOKEN": CommandLine.arguments[2],
    "AIDE_COMPUTER_BRIDGE_TOKEN_FILE": CommandLine.arguments[3],
    "AIDE_COMPUTER_BRIDGE_PORT": CommandLine.arguments[4],
    "AIDE_COMPUTER_BRIDGE_LOOPBACK_ONLY": "1"
]
let semaphore = DispatchSemaphore(value: 0)
var pid: pid_t = -1
var launchError: Error?
NSWorkspace.shared.openApplication(at: app, configuration: config) { running, error in
    pid = running?.processIdentifier ?? -1
    launchError = error
    semaphore.signal()
}
guard semaphore.wait(timeout: .now() + 20) == .success,
      launchError == nil, pid > 0 else {
    fputs("Launch Services failed: \(launchError?.localizedDescription ?? "timeout or no process")\n", stderr)
    exit(1)
}
print(pid)
SWIFT

BRIDGE_PID="$(swift "$TMP/launch.swift" "$BRIDGE_APP" "$TOKEN" "$TOKEN_FILE" "$PORT")"
for _ in $(seq 1 30); do
  if curl --silent --max-time 1 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/healthz" > "$TMP/health.json"; then break; fi
  sleep 0.5
done
python3 - "$TMP/health.json" <<'PY'
import json,sys
with open(sys.argv[1], encoding="utf-8") as f: result=json.load(f)
assert result.get("ok") is True and result.get("service") == "aide-native-computer-bridge", result
PY
test "$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 2 "http://127.0.0.1:$PORT/healthz")" = 401
ROTATED_TOKEN="$(openssl rand -hex 32)"
printf '%s' "$ROTATED_TOKEN" > "$TOKEN_FILE"
test "$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 2 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/healthz")" = 401
test "$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 2 -H "Authorization: Bearer $ROTATED_TOKEN" "http://127.0.0.1:$PORT/healthz")" = 200
lsof -nP -iTCP:"$PORT" -sTCP:LISTEN | grep -F "127.0.0.1:$PORT" >/dev/null
kill "$BRIDGE_PID"
for _ in $(seq 1 20); do
  if ! lsof -nP -iTCP:"$PORT" -sTCP:LISTEN | grep -q .; then break; fi
  sleep 0.25
done
! lsof -nP -iTCP:"$PORT" -sTCP:LISTEN | grep -q .
BRIDGE_PID=""
echo 'macOS Launch Services bridge startup/auth/token-rotation/loopback/stop smoke: PASS'
