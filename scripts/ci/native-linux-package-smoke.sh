#!/usr/bin/env bash
# Install and exercise a Debian package on an ephemeral Linux runner.
set -euo pipefail
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
sudo dpkg -r "$PKG_NAME"
printf 'Native Linux package install/start/auth/Office/stop/no-Docker smoke: PASS\n'
