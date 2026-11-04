#!/usr/bin/env bash
# Experimental Docker-free Aide runtime for macOS/Linux; no .env/Compose parsing.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${AIDE_NATIVE_BIN:-$ROOT/build/native/aide}"
[[ -x "$BIN" ]] || { echo "Native Aide binary not found: $BIN (run scripts/build-native-runtime.sh first)." >&2; exit 1; }
export AIDE_WORKSPACE="${AIDE_WORKSPACE:-$PWD}"
export AIDE_CONTEXT="${AIDE_CONTEXT:-$ROOT/context}"
if [[ -z "${AIDE_DATA:-}" ]]; then
  case "$(uname -s)" in
    Darwin) DATA_ROOT="${HOME:?}/Library/Application Support/Aide/NativeExperiment" ;;
    Linux) DATA_ROOT="${XDG_DATA_HOME:-${HOME:?}/.local/share}/aide/native-experiment" ;;
    *) echo 'Native runtime launcher supports macOS and Linux only.' >&2; exit 2 ;;
  esac
  export AIDE_DATA="$DATA_ROOT"
fi
export AIDE_ADDR="${AIDE_ADDR:-127.0.0.1:8097}"
mkdir -p "$AIDE_WORKSPACE" "$AIDE_CONTEXT" "$AIDE_DATA"
exec "$BIN"
