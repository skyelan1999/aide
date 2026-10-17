#!/usr/bin/env bash
# Host maintenance: do not start Docker, load images or execute .env.
set -euo pipefail
ROOT_LOGICAL="$(cd "$(dirname "$0")/.." && pwd -L)"
ROOT="$(cd "$ROOT_LOGICAL" && pwd -P)"
cd "$ROOT"
MODE="${1:-}"
DRY_RUN=0
if [[ "${2:-}" == --dry-run && $# == 2 ]]; then DRY_RUN=1
elif [[ $# != 1 ]]; then echo "Usage: $0 {stop|clean-cache} [--dry-run]" >&2; exit 2; fi
case "$MODE" in stop|clean-cache) ;; *) echo "Unknown maintenance action: $MODE" >&2; exit 2;; esac
DOCKER_BIN="$(command -v docker || true)"
if [[ -z "$DOCKER_BIN" && -x "$HOME/.docker/bin/docker" ]]; then DOCKER_BIN="$HOME/.docker/bin/docker"; fi
[[ -n "$DOCKER_BIN" ]] || { echo "Docker CLI not found." >&2; exit 1; }
"$DOCKER_BIN" info >/dev/null 2>&1 || { echo "Docker engine unavailable; maintenance did not start it." >&2; exit 1; }
if [[ "$MODE" == clean-cache ]]; then
  echo "Clearing unused build cache in the current Docker engine's default builder (shared by projects)."
  echo "Containers, images, volumes, settings, workspace files and offline archives are preserved."
  if (( DRY_RUN )); then echo "[dry-run] docker builder prune --all --force"; exit 0; fi
  "$DOCKER_BIN" builder prune --all --force
  exit
fi
if (( ! DRY_RUN )); then mkdir -p .cache; touch .cache/aide-manual-stop; fi
# Exact Compose working-directory + service labels avoid other AIDE copies,
# custom project names, Docmost, and unrelated containers. No Compose file needed.
IDS="$(
  for service in aide cad; do
    "$DOCKER_BIN" ps -q --filter "label=com.docker.compose.project.working_dir=$ROOT_LOGICAL" --filter "label=com.docker.compose.service=$service" || exit "$?"
    if [[ "$ROOT_LOGICAL" != "$ROOT" ]]; then
      "$DOCKER_BIN" ps -q --filter "label=com.docker.compose.project.working_dir=$ROOT" --filter "label=com.docker.compose.service=$service" || exit "$?"
    fi
  done
)"
IDS="$(printf '%s\n' "$IDS" | sort -u)"
if (( DRY_RUN )); then printf '[dry-run] stop AIDE in %s; containers: %s\n' "$ROOT" "${IDS:-none}"; exit 0; fi
if [[ -f scripts/message-lan-control.sh ]]; then bash scripts/message-lan-control.sh stop; fi
if [[ -n "$IDS" ]]; then
  while IFS= read -r id; do [[ -z "$id" ]] || "$DOCKER_BIN" stop --time 30 "$id"; done <<< "$IDS"
else echo "This AIDE copy is already stopped."; fi
if [[ "$(uname -s)" == Darwin ]]; then
  for bridge in safari-bridge-control computer-bridge-control headless-browser-control; do
    if [[ -f "scripts/$bridge.sh" ]]; then bash "scripts/$bridge.sh" stop; fi
  done
fi
echo "AIDE stopped. Data and images retained; use the normal start launcher to resume."
