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
ENGINE_READY=1
if [[ -z "$DOCKER_BIN" ]] || ! "$DOCKER_BIN" info >/dev/null 2>&1; then
  ENGINE_READY=0
  echo "Docker engine/CLI unavailable; container stop cannot be confirmed. Docker was not started." >&2
fi
if [[ "$MODE" == clean-cache ]]; then
  (( ENGINE_READY )) || exit 1
  echo "Clearing unused build cache in the current Docker engine's default builder (shared by projects)."
  echo "Containers, images, volumes, settings, workspace files and offline archives are preserved."
  if (( DRY_RUN )); then echo "[dry-run] docker builder prune --all --force"; exit 0; fi
  "$DOCKER_BIN" builder prune --all --force
  exit
fi
if (( ! DRY_RUN )); then mkdir -p .cache; touch .cache/aide-manual-stop; fi
STOP_RESULT=$(( 1 - ENGINE_READY ))
# Every service belonging to this root is part of the installation lifecycle,
# including compose-hosted third-party plugins. Never guess by port or name.
IDS=""
if (( ENGINE_READY )); then
if ! IDS="$(
  "$DOCKER_BIN" ps -q --filter "label=com.docker.compose.project.working_dir=$ROOT_LOGICAL" || exit "$?"
  if [[ "$ROOT_LOGICAL" != "$ROOT" ]]; then
    "$DOCKER_BIN" ps -q --filter "label=com.docker.compose.project.working_dir=$ROOT" || exit "$?"
  fi
)"; then STOP_RESULT=1; fi
fi
IDS="$(printf '%s\n' "$IDS" | sort -u)"
if (( DRY_RUN )); then printf '[dry-run] stop AIDE in %s; containers: %s\n' "$ROOT" "${IDS:-none}"; exit "$STOP_RESULT"; fi
# A helper failure must not prevent stopping the application or other plugins.
for helper in local-mounts-control message-lan-control; do
  if [[ -f "scripts/$helper.sh" ]]; then bash "scripts/$helper.sh" stop || STOP_RESULT=$?; fi
done
if [[ -f .aide-data/storage/data/host-components/codex-bridge.json && -f scripts/codex-host-control.cjs ]]; then
  if command -v node >/dev/null 2>&1; then node scripts/codex-host-control.cjs stop --root "$ROOT" || STOP_RESULT=$?;
  else echo 'Cannot confirm the registered Codex host component stopped: Node.js is unavailable.' >&2; STOP_RESULT=1; fi
fi
if [[ -n "$IDS" ]]; then
  while IFS= read -r id; do
    [[ -z "$id" ]] || "$DOCKER_BIN" stop --time 30 "$id" || STOP_RESULT=$?
  done <<< "$IDS"
elif (( ENGINE_READY )); then echo "This AIDE copy has no running containers."; fi
if [[ "$(uname -s)" == Darwin ]]; then
  for bridge in safari-bridge-control computer-bridge-control headless-browser-control; do
    if [[ -f "scripts/$bridge.sh" ]]; then bash "scripts/$bridge.sh" stop || STOP_RESULT=$?; fi
  done
fi
if (( STOP_RESULT )); then echo 'Some owned components did not confirm stop; inspect their local logs. Data retained.' >&2;
else echo 'AIDE stopped. Connected workbenches erase their visible content; data and images retained.'; fi
exit "$STOP_RESULT"
