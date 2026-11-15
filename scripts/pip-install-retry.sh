#!/bin/sh
# Retry complete installs and select a compatible public mainland index only
# after a transport failure. Explicit private indexes and offline installs stay put.
set -eu
log=$(mktemp)
trap 'rm -f "$log"' EXIT HUP INT TERM
index=${PIP_INDEX_URL:-https://pypi.org/simple/}
previous=
offline=0
for arg do
  [ "$previous" != index ] || index=$arg
  previous=
  case "$arg" in
    -i|--index-url) previous=index ;;
    --index-url=*) index=${arg#*=} ;;
    --no-index|--extra-index-url|--extra-index-url=*) offline=1 ;;
  esac
done
# Do not switch a private repository or mix alternate/private namespaces.
case "$index" in
  https://pypi.org/simple*|https://mirrors.aliyun.com/pypi/simple*|https://mirrors.tuna.tsinghua.edu.cn/pypi/*|https://pypi.tuna.tsinghua.edu.cn/simple*|https://mirrors.huaweicloud.com/repository/pypi/simple*) ;;
  *) offline=1 ;;
esac
[ -z "${PIP_EXTRA_INDEX_URL:-}" ] || offline=1
[ -z "${PIP_NO_INDEX:-}" ] || offline=1
run_install() {
  remaining=$#
  replace=0
  while [ "$remaining" -gt 0 ]; do
    arg=$1; shift; remaining=$((remaining - 1))
    if [ "$replace" = 1 ]; then arg=$index; replace=0; fi
    case "$arg" in
      -i|--index-url) replace=1 ;;
      --index-url=*) arg=--index-url=$index ;;
    esac
    set -- "$@" "$arg"
  done
  PIP_INDEX_URL=$index "$@"
}
attempt=1
while :; do
  if run_install "$@" >"$log" 2>&1; then cat "$log"; exit 0; else result=$?; fi
  cat "$log" >&2
  if [ "$attempt" -ge 3 ]; then
    echo '[Aide] Python dependency installation failed after 3 complete attempts. Check the configured PIP_INDEX_URL/network and retry build; no runtime was activated.' >&2
    exit "$result"
  fi
  if [ "${AIDE_CN_SOURCE_FALLBACK:-1}" != 0 ] && [ "$offline" = 0 ] && \
     grep -Eiq 'timeout|timed out|connection (reset|refused|broken|aborted)|connectionerror|newconnectionerror|name or service not known|temporary failure|network is unreachable|remote end closed|incomplete read|incompleteread|protocolerror|403|429|502|503|504' "$log"; then
    case "$index" in
      https://mirrors.tuna.tsinghua.edu.cn/*|https://pypi.tuna.tsinghua.edu.cn/*) index=https://mirrors.huaweicloud.com/repository/pypi/simple/ ;;
      *) index=https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple/ ;;
    esac
    echo "[Aide] Python download failed; matching mainland PyPI index ($attempt/2)." >&2
  fi
  echo "[Aide] Python dependency download/install failed; retrying the complete install ($attempt/3)." >&2
  attempt=$((attempt + 1))
  sleep 2
done
