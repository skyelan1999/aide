#!/usr/bin/env bash
set -euo pipefail
export AIDE_SOURCE="$(cd "$(dirname "$0")/.." && pwd)"
: "${COMPARE_TEST_ARCHIVE:?Set COMPARE_TEST_ARCHIVE to an independently built external plugin ZIP}"
export PLUGIN_MENU_EVIDENCE="${PLUGIN_MENU_EVIDENCE:-$AIDE_SOURCE/.agent-state/plugin-context-menu}"
fixture="$AIDE_SOURCE/internal/server/zz_plugin_context_menu_external_test.go"
if [ -e "$fixture" ]; then echo 'Refusing to overwrite existing host fixture' >&2; exit 1; fi
cp "$AIDE_SOURCE/scripts/fixtures/plugin-context-menu-host.go.txt" "$fixture"
trap 'rm -f "$fixture"' EXIT
cd "$AIDE_SOURCE"
go test -race ./internal/server -run '^TestPluginContextMenuExternal$' -count=1 -v -timeout 4m
