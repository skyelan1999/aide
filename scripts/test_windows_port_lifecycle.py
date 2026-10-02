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
        self.assertIn("'compose', '-f', 'compose.yaml', 'port', 'aide', '8080'", watcher)
        self.assertIn("'up', '-d', '--no-build', '--pull', 'never'", watcher)
        self.assertNotIn("down", watcher)
        self.assertNotIn("volume rm", watcher)

    def test_release_packager_includes_watcher_in_windows_and_full_bundles(self):
        packager = (ROOT / "scripts/package-release-assets.sh").read_text()
        self.assertIn('if [[ "$TARGET" == windows-arm64 ]]; then cp "$ROOT/scripts/watch-port.ps1"', packager)
        self.assertIn('"$ROOT/scripts/watch-port.sh" "$ROOT/scripts/watch-port.ps1" "$ROOT/scripts/update-agent.sh"', packager)


if __name__ == "__main__":
    unittest.main()
