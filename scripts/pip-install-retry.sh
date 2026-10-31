#!/bin/sh
# Retry the whole install: pip's transport retries do not cover every aborted
# wheel-body read. Keep the caller's exact pinned requirements/index/options.
set -eu
attempt=1
while :; do
  if "$@"; then exit 0; else result=$?; fi
  if [ "$attempt" -ge 3 ]; then
    echo '[Aide] Python dependency installation failed after 3 complete attempts. Check the configured PIP_INDEX_URL/network and retry build; no runtime was activated.' >&2
    exit "$result"
  fi
  echo "[Aide] Python dependency download/install failed; retrying the complete install ($attempt/3)." >&2
  attempt=$((attempt + 1))
  sleep 2
done
