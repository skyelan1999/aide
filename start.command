#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
umask 077
mkdir -p .agent-state/startup
START_LOG=".agent-state/startup/start-$(date '+%Y%m%dT%H%M%S')-$$.log"
MODE=start
if [[ -f .aide-image ]]; then MODE=start-bundle; fi
# Keep the exact double-click entry observable even when startup fails before
# the browser opens. The log is local/private and never uploaded automatically.
set +e
: > "$START_LOG"
bash scripts/aide.sh "$MODE" > >(tee -a "$START_LOG") 2> >(tee -a "$START_LOG" >&2)
START_RESULT=$?
set -e
echo "Startup log: $PWD/$START_LOG"
exit "$START_RESULT"
