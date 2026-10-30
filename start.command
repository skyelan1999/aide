#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
umask 077
mkdir -p .agent-state/startup
START_LOG=".agent-state/startup/start-$(date '+%Y%m%dT%H%M%S')-$$.log"
MODE=start
# Keep the exact double-click entry observable even when startup fails before
# the browser opens. The log is local/private and never uploaded automatically.
set +e
: > "$START_LOG"
bash scripts/aide.sh "$MODE" 2>&1 | tee -a "$START_LOG"
START_RESULT=${PIPESTATUS[0]}
set -e
echo "Startup log: $PWD/$START_LOG"
exit "$START_RESULT"
