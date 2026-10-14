#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
exec bash scripts/aide-maintenance.sh stop "$@"
