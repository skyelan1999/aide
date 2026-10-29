#!/usr/bin/env python3
"""Static regression checks for the Windows offline launcher port lifecycle."""
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class WindowsPortLifecycleTests(unittest.TestCase):
    def test_launcher_selects_and_persists_fallback_port(self):
        launcher = (ROOT / "start.ps1").read_text(encoding="utf-8-sig")
        self.assertIn("Test-LocalHostPortAvailable $configuredPort", launcher)
        self.assertIn("$candidate -le [Math]::Min($configuredPort + 100, 65535)", launcher)
        self.assertIn('"AIDE_PORT=$selectedPort"', launcher)

    def test_launcher_starts_host_port_watcher_for_offline_bundles(self):
        launcher = (ROOT / "start.ps1").read_text(encoding="utf-8-sig")
        self.assertIn("scripts/watch-port.ps1", launcher)
        self.assertIn("if ($offlineBundle -and (Test-Path", launcher)
        self.assertIn("('\"{0}\"' -f $portWatcherPath)", launcher)
        self.assertIn("('\"{0}\"' -f $updateAgentPath)", launcher)

    def test_watcher_applies_saved_port_without_changing_shared_data_mounts(self):
        watcher = (ROOT / "scripts/watch-port.ps1").read_text(encoding="utf-8-sig")
        self.assertIn("cat /data/config/host-port", watcher)
        self.assertIn("'compose', 'port', 'aide', '8080'", watcher)
        self.assertIn("'up', '-d', '--no-build', '--pull', 'never'", watcher)
        self.assertNotIn("down", watcher)
        self.assertNotIn("volume rm", watcher)

    def test_host_agents_keep_launcher_compose_overrides(self):
        for name in ('watch-port.ps1', 'update-agent.ps1'):
            text = (ROOT / 'scripts' / name).read_text(encoding='utf-8-sig')
            self.assertIn("if (-not $env:COMPOSE_FILE) { $env:COMPOSE_FILE = 'compose.yaml' }", text)
            self.assertNotIn("'-f', 'compose.yaml'", text)
            self.assertNotIn('compose -f compose.yaml', text)
        text = (ROOT / 'scripts/update-agent.sh').read_text()
        self.assertIn('COMPOSE_FILE="${COMPOSE_FILE:-compose.yaml}"', text)

    def test_native_entrypoints_and_packaging_replace_legacy_agents(self):
        # Wiring check only; runtime transaction and Win32 tests are separate.
        for name, action in [('start.bat', 'start'), ('stop.bat', 'stop'), ('clean-docker-cache.bat', 'clean-cache')]:
            text = (ROOT / name).read_text()
            self.assertIn('"%~dp0aide-launcher.exe" ' + action + ' %*', text)
            self.assertNotIn('.ps1', text)
            self.assertNotIn('powershell', text.lower())
        packager = (ROOT / "scripts/package-release-assets.sh").read_text()
        self.assertIn('LAUNCHERS=(start.bat stop.bat clean-docker-cache.bat)', packager)
        self.assertIn('"$WINDOWS_LAUNCHER" "$PACKAGE/aide-launcher.exe"', packager)
        self.assertIn('"$ROOT/scripts/watch-port.sh"', packager)
        self.assertNotIn('.ps1', packager)



if __name__ == "__main__":
    unittest.main()
