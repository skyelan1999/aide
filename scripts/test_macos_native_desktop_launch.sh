#!/usr/bin/env bash
set -euo pipefail

APP="${1:?usage: test_macos_native_desktop_launch.sh /path/to/Aide.app /path/to/mock-daemon-fixture}"
FIXTURE="${2:?usage: test_macos_native_desktop_launch.sh /path/to/Aide.app /path/to/mock-daemon-fixture}"
[[ -d "$APP" && -x "$APP/Contents/MacOS/AideDesktop" && -x "$APP/Contents/Resources/aide-server" ]] || {
  echo "Incomplete native Aide app: $APP" >&2
  exit 2
}
command -v swift >/dev/null
command -v curl >/dev/null

TMP="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-desktop.XXXXXX")"
APP_PID=""
SERVER_PID=""
cleanup() {
  if [[ -n "$APP_PID" ]]; then swift "$TMP/lifecycle.swift" quit "$APP_PID" >/dev/null 2>&1 || true; fi
  if [[ -n "$SERVER_PID" ]]; then kill "$SERVER_PID" 2>/dev/null || true; fi
  python3 - "$TMP" <<'PY'
import shutil,sys
shutil.rmtree(sys.argv[1], ignore_errors=True)
PY
}
trap cleanup EXIT
mkdir -p "$TMP/workspace" "$TMP/context" "$TMP/data"
mkdir -p "$TMP/workspace/plugins/native-runtime-smoke-daemon"
cp "$FIXTURE/index.js" "$TMP/workspace/plugins/native-runtime-smoke-daemon/index.js"
cat > "$TMP/workspace/plugins/registry.json" <<'JSON'
{"version":1,"plugins":[{"id":"native-runtime-smoke-daemon","name":"Native Runtime Smoke Daemon","version":"0.0.1","main":"index.js","daemon":true,"enabled":false}]}
JSON

cat > "$TMP/lifecycle.swift" <<'SWIFT'
import AppKit
import Foundation
if CommandLine.arguments[1] == "quit" {
    let pid = pid_t(CommandLine.arguments[2]) ?? -1
    guard let app = NSRunningApplication(processIdentifier: pid) else { exit(0) }
    app.terminate()
    exit(0)
}
let identifier = "local.aide.desktop.native"
if NSRunningApplication.runningApplications(withBundleIdentifier: identifier).contains(where: { !$0.isTerminated }) {
    fputs("Refusing to attach to an existing native Aide process.\n", stderr)
    exit(2)
}
let config = NSWorkspace.OpenConfiguration()
config.activates = true
config.environment = [
    "AIDE_NATIVE_WORKSPACE": CommandLine.arguments[2] + "/workspace",
    "AIDE_NATIVE_CONTEXT": CommandLine.arguments[2] + "/context",
    "AIDE_NATIVE_DATA_ROOT": CommandLine.arguments[2] + "/data"
]
let semaphore = DispatchSemaphore(value: 0)
var pid: pid_t = -1
var launchError: Error?
NSWorkspace.shared.openApplication(at: URL(fileURLWithPath: CommandLine.arguments[1]), configuration: config) { running, error in
    pid = running?.processIdentifier ?? -1
    launchError = error
    semaphore.signal()
}
guard semaphore.wait(timeout: .now() + 20) == .success, launchError == nil, pid > 0 else {
    fputs("Aide Launch Services failed: \(launchError?.localizedDescription ?? "timeout")\n", stderr)
    exit(1)
}
print(pid)
SWIFT

APP_PID="$(swift "$TMP/lifecycle.swift" "$APP" "$TMP")"
DATA="$TMP/data"
for _ in $(seq 1 75); do
  SERVER_PID="$(python3 - "$APP/Contents/Resources/aide-server" <<'PY'
import ctypes,os,subprocess,sys
lib=ctypes.CDLL('/usr/lib/libproc.dylib')
lib.proc_pidpath.argtypes=[ctypes.c_int,ctypes.c_void_p,ctypes.c_uint32]
expected=os.path.realpath(sys.argv[1])
for raw in subprocess.check_output(['/bin/ps','-axo','pid='],text=True).splitlines():
    try: pid=int(raw.strip())
    except ValueError: continue
    buf=ctypes.create_string_buffer(4096)
    if lib.proc_pidpath(pid,buf,len(buf))>0 and os.path.realpath(os.fsdecode(buf.value))==expected:
        print(pid); break
PY
  )"
  if [[ -f "$DATA/logs/server.log" ]]; then
    PORT="$(python3 - "$DATA/logs/server.log" <<'PY'
import re,sys
text=open(sys.argv[1],errors='replace').read()
matches=re.findall(r'aide listening on https://127\.0\.0\.1:(\d+)',text)
print(matches[-1] if matches else '')
PY
    )"
    if [[ -n "$SERVER_PID" && -n "$PORT" ]] && curl --silent --insecure --fail --max-time 1 "https://localhost:$PORT/healthz" > "$TMP/health.json" 2>/dev/null; then break; fi
  fi
  sleep 1
done
[[ -n "$SERVER_PID" && -n "${PORT:-}" && -s "$TMP/health.json" ]] || {
  echo "Native desktop service failed to become healthy: $DATA/logs/server.log" >&2
  cat "$DATA/logs/server.log" 2>/dev/null || true
  exit 1
}
python3 - "$TMP/health.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1],encoding='utf-8'))
assert value.get('status')=='ok' and value.get('service')=='aide', value
PY
TOKEN="$(cat "$DATA/auth/access-token")"
curl --silent --insecure --fail --max-time 3 -H "Authorization: Bearer $TOKEN" "https://localhost:$PORT/api/config" > "$TMP/config.json"
python3 - "$TMP/config.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1],encoding='utf-8'))
assert value.get('runtimeMode')=='native', value
print('runtimeMode=native')
PY
"$APP/Contents/Resources/runtime/python/bin/python3.12" -I \
  "$(cd "$(dirname "$0")/.." && pwd)/scripts/ci/native-plugin-daemon-smoke.py" \
  --base "https://localhost:$PORT" --token "$TOKEN" --workspace "$TMP/workspace" --fixture "$FIXTURE"
BRIDGE_PID="$(python3 - "$APP/Contents/Resources/Aide Computer Bridge.app/Contents/MacOS/AideComputerBridge" <<'PY'
import ctypes,os,subprocess,sys
lib=ctypes.CDLL('/usr/lib/libproc.dylib')
lib.proc_pidpath.argtypes=[ctypes.c_int,ctypes.c_void_p,ctypes.c_uint32]
expected=os.path.realpath(sys.argv[1])
for raw in subprocess.check_output(['/bin/ps','-axo','pid='],text=True).splitlines():
    try: pid=int(raw.strip())
    except ValueError: continue
    buf=ctypes.create_string_buffer(4096)
    if lib.proc_pidpath(pid,buf,len(buf))>0 and os.path.realpath(os.fsdecode(buf.value))==expected:
        print(pid); break
PY
)"
[[ -n "$BRIDGE_PID" ]] || { echo 'Aide app did not start its bundled computer bridge' >&2; exit 1; }
echo 'Native Aide.app Launch Services/service/auth/bridge smoke: PASS'
swift "$TMP/lifecycle.swift" quit "$APP_PID"
for _ in $(seq 1 20); do
  if ! kill -0 "$APP_PID" 2>/dev/null && ! kill -0 "$SERVER_PID" 2>/dev/null && ! kill -0 "$BRIDGE_PID" 2>/dev/null; then break; fi
  sleep 0.5
done
if kill -0 "$APP_PID" 2>/dev/null || kill -0 "$SERVER_PID" 2>/dev/null || kill -0 "$BRIDGE_PID" 2>/dev/null; then
  echo 'Native Aide app did not stop all child processes' >&2
  exit 1
fi
APP_PID=""
SERVER_PID=""
BRIDGE_PID=""
echo 'Native Aide.app graceful shutdown smoke: PASS'
