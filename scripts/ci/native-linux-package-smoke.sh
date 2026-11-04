#!/usr/bin/env bash
# Install and exercise a Debian package on an ephemeral Linux runner.
set -euo pipefail
if [[ "${AIDE_NATIVE_LINUX_XVFB:-}" != 1 ]]; then
  command -v sudo >/dev/null && sudo apt-get update -qq && sudo apt-get install -y -qq xvfb xauth x11-utils xterm
  exec xvfb-run -a env AIDE_NATIVE_LINUX_XVFB=1 XDG_SESSION_TYPE=x11 "$0" "$@"
fi
PACKAGE="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
[[ -f "$PACKAGE" ]] || { echo "Debian package not found: $PACKAGE" >&2; exit 2; }
command -v sudo >/dev/null && command -v dpkg-deb >/dev/null || { echo 'sudo and dpkg-deb are required.' >&2; exit 2; }
PKG_NAME="$(dpkg-deb --field "$PACKAGE" Package)"
if dpkg-query -W -f='${Status}' "$PKG_NAME" 2>/dev/null | grep -q 'install ok installed'; then
  echo "$PKG_NAME is already installed; run this smoke test on a disposable host." >&2
  exit 2
fi
sudo apt-get update -qq
sudo apt-get install -y -qq "$PACKAGE"
TEST_ROOT="${RUNNER_TEMP:-$(mktemp -d)}/aide-native-linux-package-smoke"
mkdir -p "$TEST_ROOT"/{bin,home,data,workspace,context}
xterm_pid=''
cleanup_native_smoke() {
  if [[ -n "$xterm_pid" ]]; then kill "$xterm_pid" 2>/dev/null || true; fi
  aide-native-stop >/dev/null 2>&1 || true
}
trap cleanup_native_smoke EXIT
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$TEST_ROOT/workspace/plugins/native-runtime-smoke-daemon"
cp "$REPO_ROOT/plugins/testdata/mock-daemon/index.js" "$TEST_ROOT/workspace/plugins/native-runtime-smoke-daemon/index.js"
cat >"$TEST_ROOT/workspace/plugins/registry.json" <<'JSON'
{"version":1,"plugins":[{"id":"native-runtime-smoke-daemon","name":"Native Runtime Smoke Daemon","version":"0.0.1","main":"index.js","daemon":true,"enabled":false}]}
JSON
cat >"$TEST_ROOT/bin/docker" <<'SH'
#!/bin/sh
echo "unexpected Docker invocation: $*" >>"$AIDE_NATIVE_DOCKER_MARKER"
exit 97
SH
cat >"$TEST_ROOT/bin/xdg-open" <<'SH'
#!/bin/sh
printf '%s\n' "$1" >"$AIDE_NATIVE_BROWSER_URL"
SH
chmod 755 "$TEST_ROOT/bin/docker" "$TEST_ROOT/bin/xdg-open"
export HOME="$TEST_ROOT/home" AIDE_DATA="$TEST_ROOT/data" AIDE_WORKSPACE="$TEST_ROOT/workspace" AIDE_CONTEXT="$TEST_ROOT/context"
export AIDE_ADDR=127.0.0.1:18097 AIDE_NATIVE_BROWSER_URL="$TEST_ROOT/browser-url" AIDE_NATIVE_DOCKER_MARKER="$TEST_ROOT/docker-called"
export PATH="$TEST_ROOT/bin:$PATH"
aide-native-ui
grep -q '^http://localhost:18097/#token=' "$AIDE_NATIVE_BROWSER_URL"
token="$(cat "$AIDE_DATA/auth/access-token")"
runtime=/usr/lib/aide-native/runtime
test -f /usr/lib/aide-native/computer-bridge-linux.js
"$runtime/node/bin/node" --check /usr/lib/aide-native/computer-bridge-linux.js
curl -fsS "http://127.0.0.1:18097/healthz" | "$runtime/python/bin/python3.12" -I -c 'import json,sys; h=json.load(sys.stdin); assert h["status"]=="ok" and h["integrity"]=="ok",h'
curl -fsS -H "Authorization: Bearer $token" http://127.0.0.1:18097/api/config | "$runtime/python/bin/python3.12" -I -c 'import json,sys; assert json.load(sys.stdin)["runtimeMode"] == "native"'
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18097/)" == 200 ]]
test -s "$AIDE_DATA/computer-bridge.pid" && test -s "$AIDE_DATA/auth/computer-bridge-token"
bridge_token="$(cat "$AIDE_DATA/auth/computer-bridge-token")"
curl -fsS -H "Authorization: Bearer $bridge_token" http://127.0.0.1:17778/healthz | \
  "$runtime/python/bin/python3.12" -I -c 'import json,sys; h=json.load(sys.stdin); assert h["ok"] and h["service"] == "aide-linux-computer-bridge",h'
test "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:17778/healthz)" = 401
xterm -class AideNativeSmoke -T AideNativeSmoke -e sh -c 'printf "AIDE native screenshot smoke\\n"; sleep 90' >"$TEST_ROOT/xterm.log" 2>&1 &
xterm_pid=$!
window_id=''
for _ in $(seq 1 30); do
  window_id="$(xdotool search --onlyvisible --name 'AideNativeSmoke' 2>/dev/null | head -1 || true)"
  [[ -n "$window_id" ]] && break
  if ! kill -0 "$xterm_pid" 2>/dev/null; then break; fi
  sleep 0.2
done
if [[ -z "$window_id" ]]; then
  echo 'X11 screenshot fixture window did not appear.' >&2
  cat "$TEST_ROOT/xterm.log" >&2 || true
  exit 1
fi
xdotool windowfocus --sync "$window_id"
target_pid="$(xdotool getwindowpid "$window_id")"
target_name="$(cat "/proc/$target_pid/comm")"
printf 'X11 screenshot target: id=%s pid=%s process=%s focused=%s\n' "$window_id" "$target_pid" "$target_name" "$(xdotool getwindowfocus)"
"$runtime/python/bin/python3.12" -I - "$bridge_token" <<'PY'
import base64,json,sys,urllib.request
from urllib.error import HTTPError
req=urllib.request.Request('http://127.0.0.1:17778/v1/snapshot',data=json.dumps({'allowedApps':['xterm','AideNativeSmoke']}).encode(),headers={'Authorization':'Bearer '+sys.argv[1],'Content-Type':'application/json'},method='POST')
try:
    with urllib.request.urlopen(req,timeout=15) as response:
        result=json.load(response)
except HTTPError as error:
    print(f'computer bridge HTTP {error.code}: {error.read().decode("utf-8",errors="replace")}',file=sys.stderr)
    raise
import subprocess
image=base64.b64decode(result.get('imageBase64',''),validate=True)
assert result.get('ok') is True and result.get('mimeType')=='image/png',result
assert len(image)>100 and image[:8]==b'\x89PNG\r\n\x1a\n',(len(image),image[:8])
width,height=__import__('struct').unpack('>II',image[16:24])
assert width>=100 and height>=100,(width,height)
identified=subprocess.run(['identify','-format','%k','png:-'],input=image,stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=True).stdout.decode().strip()
colors=int(identified)
assert colors>=2,colors
print(f'X11 packaged screenshot: PASS ({width}x{height}, {colors} colors, {len(image)} PNG bytes)')
PY
kill "$xterm_pid" 2>/dev/null || true
wait "$xterm_pid" 2>/dev/null || true
"$runtime/python/bin/python3.12" -I "$REPO_ROOT/scripts/ci/native-plugin-daemon-smoke.py" \
  --base http://127.0.0.1:18097 --token "$token" --workspace "$TEST_ROOT/workspace" \
  --fixture "$REPO_ROOT/plugins/testdata/mock-daemon"
"$runtime/python/bin/python3.12" -I "$REPO_ROOT/scripts/ci/native-ai-session-smoke.py" \
  --base http://127.0.0.1:18097 --token "$token"
printf '%s\n' 'native-command-payload-4f9a' >"$TEST_ROOT/outside-read.txt"
command_json="$("$runtime/python/bin/python3.12" -I -c 'import json,sys; print(json.dumps({"command":sys.argv[1]}))' "node -e 'require(\"fs\").writeFileSync(\"native-command.txt\",\"sandbox-ok\")' && cat native-command.txt")"
curl -fsS -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$command_json" http://127.0.0.1:18097/api/command >"$TEST_ROOT/workspace-command.ndjson"
"$runtime/python/bin/python3.12" -I - "$TEST_ROOT/workspace-command.ndjson" "$TEST_ROOT/workspace/native-command.txt" <<'PY'
import json,sys
rows=[json.loads(line) for line in open(sys.argv[1]) if line.strip()]
assert any(row.get('text')=='sandbox-ok' for row in rows), rows
assert any(row.get('type')=='exit' and row.get('code')==0 for row in rows), rows
assert open(sys.argv[2]).read()=='sandbox-ok'
PY
outside_command="$("$runtime/python/bin/python3.12" -I -c 'import json,shlex,sys; print(json.dumps({"command":"cat "+shlex.quote(sys.argv[1])}))' "$TEST_ROOT/outside-read.txt")"
curl -fsS -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$outside_command" http://127.0.0.1:18097/api/command >"$TEST_ROOT/outside-command.ndjson"
"$runtime/python/bin/python3.12" -I - "$TEST_ROOT/outside-command.ndjson" <<'PY'
import json,sys
rows=[json.loads(line) for line in open(sys.argv[1]) if line.strip()]
output=''.join(row.get('text','') for row in rows if row.get('type')=='output')
assert 'native-command-payload-4f9a' not in output, output
assert any(row.get('type')=='exit' and row.get('code')!=0 for row in rows), rows
PY
outside_write_command="$("$runtime/python/bin/python3.12" -I -c 'import json,shlex,sys; print(json.dumps({"command":"printf leaked > "+shlex.quote(sys.argv[1])}))' "$TEST_ROOT/outside-write.txt")"
curl -fsS -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$outside_write_command" http://127.0.0.1:18097/api/command >"$TEST_ROOT/outside-write-command.ndjson"
"$runtime/python/bin/python3.12" -I - "$TEST_ROOT/outside-write-command.ndjson" "$TEST_ROOT/outside-write.txt" <<'PY'
import json,os,sys
rows=[json.loads(line) for line in open(sys.argv[1]) if line.strip()]
assert not os.path.exists(sys.argv[2]), sys.argv[2]
assert any(row.get('type')=='exit' and row.get('code')!=0 for row in rows), rows
PY
"$runtime/node/bin/node" --version | grep -q '^v24\.'
"$runtime/python/bin/python3.12" -I -c 'import docx,openpyxl,pptx,ezdxf,pypdf'
printf '%s\n' '{"title":"Native install smoke","blocks":[{"text":"without Docker"}]}' >"$TEST_ROOT/spec.json"
"$runtime/python/bin/python3.12" -I /usr/lib/aide-native/office-scripts/office_artifact.py create docx "$TEST_ROOT/spec.json" "$TEST_ROOT/smoke.docx"
"$runtime/python/bin/python3.12" -I /usr/lib/aide-native/office-scripts/extract_text.py "$TEST_ROOT/smoke.docx" | grep -q 'without Docker'
aide-native-stop
code=000
for _ in $(seq 1 20); do
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 http://127.0.0.1:18097/healthz || true)"
  [[ "$code" == 000 ]] && break
  sleep 0.5
done
[[ "$code" == 000 ]] || { echo "Aide still responds after stop: HTTP $code" >&2; exit 1; }
[[ ! -e "$AIDE_NATIVE_DOCKER_MARKER" ]]
trap - EXIT
sudo dpkg -r "$PKG_NAME"
printf 'Native Linux package install/start/auth/Office/stop/no-Docker smoke: PASS\n'
