#!/usr/bin/env bash
# Host-only companion is extracted from the selected immutable image.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:-start}"; IMAGE="${2:-}"
DOCKER_BIN="${DOCKER_BIN:-$(command -v docker)}"
case "$(uname -s)" in Darwin) os=darwin;; Linux) os=linux;; *) exit 2;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) exit 2;; esac
NATIVE="$ROOT/.aide-data/local-mounts"; NAME="aide-local-mounts-$os-$arch"; BIN="$NATIVE/$NAME"
mkdir -p "$NATIVE";chmod 700 "$NATIVE"
# Fail closed for malformed/present state; only an empty or absent configuration may skip.
empty_mounts(){ python3 - "$ROOT" <<'PYCHECK'
import json,sys
from pathlib import Path
root=Path(sys.argv[1])
for name in ('.aide-data/storage/data/local-mounts/desired.json','.aide-data/local-mounts/active.json'):
 p=root/name
 if p.exists():
  try:
   value=json.loads(p.read_text())
   if not isinstance(value,dict) or not isinstance(value.get('entries'),list) or value['entries']:sys.exit(1)
  except Exception:sys.exit(1)
PYCHECK
}
skip_unavailable(){
 empty_mounts || { echo 'Selected image has no directory helper but local mounts are configured; update the image before starting.' >&2;return 1; }
 if [[ -x "$BIN" ]];then "$BIN" -mode stop -root "$ROOT";fi
 touch "$NATIVE/unavailable"
 echo 'Selected image has no directory helper; no local mounts configured, continuing without the optional helper.' >&2
}
if [[ "$MODE" == stop ]];then [[ ! -x "$BIN" ]] || "$BIN" -mode stop -root "$ROOT";exit;fi
if [[ "$MODE" == prepare ]];then
 [[ -n "$IMAGE" ]] || { echo 'Image required for directory helper extraction' >&2;exit 1; }
 TMP="$(mktemp -d "$NATIVE/extract.XXXXXX")";SOURCE=''
 cleanup(){ [[ -z "$SOURCE" ]] || "$DOCKER_BIN" rm "$SOURCE" >/dev/null 2>&1 || true;rm -rf "$TMP"; };trap cleanup EXIT
 SOURCE="$("$DOCKER_BIN" create --network none --entrypoint /bin/true "$IMAGE")"
 if ! "$DOCKER_BIN" cp "$SOURCE:/opt/aide/message-lan/$NAME" "$TMP/$NAME" >/dev/null 2>&1;then skip_unavailable;exit;fi
 "$DOCKER_BIN" cp "$SOURCE:/opt/aide/message-lan/SHA256SUMS" "$TMP/SHA256SUMS" >/dev/null
 expected="$(awk -v name="$NAME" '$2==name {print $1}' "$TMP/SHA256SUMS")"
 actual="$(shasum -a 256 "$TMP/$NAME" | awk '{print $1}')"
 [[ "$expected" =~ ^[a-f0-9]{64}$ && "$expected" == "$actual" ]] || { echo 'Directory helper checksum mismatch' >&2;exit 1; }
 if [[ -x "$BIN" ]];then "$BIN" -mode stop -root "$ROOT";fi
 chmod 700 "$TMP/$NAME";mv "$TMP/$NAME" "$BIN"
 rm -f "$NATIVE/unavailable"
 "$BIN" -mode prepare -root "$ROOT" -docker "$DOCKER_BIN"
elif [[ "$MODE" == start ]];then
 if [[ -f "$NATIVE/unavailable" || ! -x "$BIN" ]];then skip_unavailable;exit;fi
 python3 - "$BIN" "$ROOT" "$DOCKER_BIN" "$NATIVE/helper.log" <<'PY'
import subprocess,sys
binary,root,docker,log=sys.argv[1:]
with open(log,'ab') as output:
 subprocess.Popen([binary,'-root',root,'-docker',docker],stdin=subprocess.DEVNULL,stdout=output,stderr=output,start_new_session=True,close_fds=True)
PY
else exit 2;fi
