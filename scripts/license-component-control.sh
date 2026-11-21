#!/bin/sh
# Reopen an already installed PP user service using its own trusted installation.
set -eu
[ "$(uname -s)" = Darwin ] || exit 0
service=com.skyelan.aide.pp-offline-license
plist="$HOME/Library/LaunchAgents/$service.plist"
[ -f "$plist" ] || exit 0
if [ "${1:-start}" = prepare ]; then
  bridge="$HOME/Library/Application Support/aide/pp-offline-license/bridge"
  [ -d "$bridge" ] || exit 0
  python3 - "$bridge" "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)" <<'PYTHON'
import json, os, pathlib, sys
bridge, root = map(pathlib.Path, sys.argv[1:])
state = root/'.aide-data'
destination = state/'pp-license.compose.json'
if bridge.is_symlink() or state.is_symlink() or destination.is_symlink():
    raise SystemExit('Refusing a symlink in license bridge configuration')
state.mkdir(exist_ok=True)
temporary = destination.with_suffix('.tmp')
temporary.write_text(json.dumps({'services': {'aide': {'volumes': [{
    'type': 'bind', 'source': str(bridge), 'target': '/run/pp-license', 'read_only': True,
    'bind': {'create_host_path': False}
}]}}}))
os.chmod(temporary, 0o600)
temporary.replace(destination)
print(destination)
PYTHON
  exit 0
fi
domain="gui/$(id -u)"
if ! launchctl print "$domain/$service" >/dev/null 2>&1; then
  launchctl bootstrap "$domain" "$plist" || {
    echo 'PP 授权组件启动失败，请检查组件安装日志。' >&2
    exit 1
  }
fi
# kickstart without -k preserves any running license process and device identity.
launchctl kickstart "$domain/$service" || {
  echo 'PP 授权组件未能启动，请检查组件安装日志。' >&2
  exit 1
}
