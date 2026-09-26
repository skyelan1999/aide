#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if [[ -f .aide-image ]]; then
  exec bash scripts/aide.sh start-bundle
fi
exec bash scripts/aide.sh start
