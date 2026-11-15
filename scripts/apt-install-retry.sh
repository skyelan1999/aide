#!/bin/sh
# Build-only Debian recovery. Preserve suites, keys, components and package pins.
set -eu
sources=${AIDE_APT_SOURCES_DIR:-/etc/apt}
log=$(mktemp)
trap 'rm -f "$log"' EXIT HUP INT TERM
attempt=0
while :; do
  if (apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 \
        -o APT::Update::Error-Mode=any update && \
      apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 \
        install -y --no-install-recommends "$@") >"$log" 2>&1; then
    cat "$log"; exit 0
  else result=$?; fi
  cat "$log" >&2
  if [ "${AIDE_CN_SOURCE_FALLBACK:-1}" = 0 ] || [ "$attempt" -ge 2 ] || \
     ! grep -Eiq 'failed to fetch|could not resolve|temporary failure resolving|connection (failed|timed out|refused|reset)|unable to connect|network is unreachable|timeout|timed out|TLS connection' "$log"; then
    exit "$result"
  fi
  attempt=$((attempt + 1))
  case "$attempt" in
    1) mirror=https://mirrors.tuna.tsinghua.edu.cn ;;
    2) mirror=https://mirrors.aliyun.com ;;
  esac
  changed=0
  for file in "$sources/sources.list" "$sources"/sources.list.d/*.list "$sources"/sources.list.d/*.sources; do
    [ -f "$file" ] || continue
    if grep -Eq 'https?://(deb\.debian\.org|security\.debian\.org|mirrors\.tuna\.tsinghua\.edu\.cn|mirrors\.aliyun\.com)/debian' "$file"; then
      sed -E "s#https?://(deb\.debian\.org|security\.debian\.org|mirrors\.tuna\.tsinghua\.edu\.cn|mirrors\.aliyun\.com)/debian(-security)?#${mirror}/debian\2#g" "$file" >"$log"
      cat "$log" >"$file"
      changed=1
    fi
  done
  [ "$changed" = 1 ] || exit "$result"
  echo "[Aide] Debian download failed; retrying with $mirror ($attempt/2)." >&2
done
