#!/usr/bin/env python3
"""Cross-platform checks for the native-only Windows distribution contract."""
from pathlib import Path
import importlib.util
import unittest

ROOT = Path(__file__).resolve().parents[1]


class NativeDistributionTests(unittest.TestCase):
    def test_no_powershell_sources_or_entrypoint_fallback(self):
        files = list(ROOT.glob('*.ps1')) + list((ROOT / 'scripts').rglob('*.ps1'))
        self.assertEqual(files, [])
        for name in ('start.bat', 'stop.bat', 'clean-docker-cache.bat'):
            text = (ROOT / name).read_text().lower()
            self.assertIn('aide-launcher.exe', text)
            self.assertNotIn('powershell', text)
            self.assertNotIn('.ps1', text)

    def test_full_package_requires_executable(self):
        spec = importlib.util.spec_from_file_location('candidate', ROOT / 'scripts/ci/build_candidate.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertIn('aide-launcher.exe', module.FULL_PACKAGE_STARTUP_FILES)
        self.assertFalse(any(path.endswith('.ps1') for path in module.FULL_PACKAGE_STARTUP_FILES))
        for name in ('scripts/package-release-assets.sh', 'scripts/aide.sh'):
            text = (ROOT / name).read_text()
            self.assertIn('windows-launcher-asset.sh', text)
            self.assertIn('aide-launcher.exe', text)
            self.assertNotIn('.ps1', text)

    def test_forwarder_refresh_uses_native_windows_entry(self):
        text = (ROOT / 'cmd/aide-local-mounts/main.go').read_text()
        self.assertIn('"refresh-helpers"', text)
        self.assertNotIn('powershell.exe', text)
        self.assertNotIn('".ps1"', text)


if __name__ == '__main__':
    unittest.main()
