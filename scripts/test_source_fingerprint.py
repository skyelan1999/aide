"""Source identity stays stable across generated plugin catalogue refreshes."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]

class SourceFingerprintTests(unittest.TestCase):
    def test_caller_collation_does_not_change_source_identity(self):
        # Require a real differing host collation, rather than a locale name
        # silently falling back to C on machines without locale data.
        sample = 'internal/A.go\ninternal/a.go\n'
        def sorted_paths(locale):
            return subprocess.check_output(['sort'], input=sample, text=True,
                                           env={**os.environ, 'LC_ALL': locale})
        baseline_order = sorted_paths('C')
        locales = subprocess.check_output(['locale', '-a'], text=True).splitlines()
        alternate = next((name for name in locales
                          if 'utf' in name.lower() and sorted_paths(name) != baseline_order), None)
        if alternate is None:
            self.skipTest('Host has no UTF-8 locale with different path collation')
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for name in ['cmd', 'internal', 'vendor', 'scripts/office', 'scripts/cad', 'docker/cad', 'docker/wheels', 'docker/sherpa']:
                (root/name).mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT/'scripts/aide.sh', root/'scripts/aide.sh')
            for name in ('launcher-data.sh','launcher-data.ps1','launcher-data.py'):
                shutil.copy2(ROOT/'scripts'/name, root/'scripts'/name)
            for name in ['go.mod', 'go.sum', 'Dockerfile', 'compose.yaml', '.dockerignore', 'version.md',
                         'internal/A.go', 'internal/a.go']:
                (root/name).write_text(name)
            def fingerprint(locale):
                return subprocess.check_output(['bash', 'scripts/aide.sh', 'source-sha'], cwd=root,
                                               text=True, env={**os.environ, 'LC_ALL': locale}, timeout=10)
            self.assertEqual(fingerprint('C'), fingerprint(alternate), alternate)
            first = fingerprint(alternate)
            (root/'internal/a.go').write_text('changed source')
            self.assertNotEqual(first, fingerprint('C'))

    def test_installed_plugin_state_is_excluded(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for name in ['cmd', 'internal', 'plugins', 'vendor', 'scripts/office', 'scripts/cad', 'docker/cad', 'docker/wheels', 'docker/sherpa']:
                (root/name).mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT/'scripts/aide.sh', root/'scripts/aide.sh')
            for name in ('launcher-data.sh','launcher-data.ps1','launcher-data.py'):
                shutil.copy2(ROOT/'scripts'/name, root/'scripts'/name)
            for name in ['go.mod', 'go.sum', 'Dockerfile', 'compose.yaml', '.dockerignore', 'version.md']:
                (root/name).write_text(name)
            (root/'internal/file.go').write_text('package fixture\n')
            def fingerprint():
                result = subprocess.run(['bash', 'scripts/aide.sh', 'source-sha'], cwd=root, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertRegex(result.stdout, r'^[0-9a-f]{64}\n$')
                return result.stdout
            first = fingerprint()
            (root/'internal/.DS_Store').write_bytes(b'Finder view state')
            self.assertEqual(first, fingerprint())
            (root/'internal/.DS_Store').write_bytes(b'Finder changed during build')
            self.assertEqual(first, fingerprint())
            (root/'plugins/surface.json').write_text('{"generatedAt":"before"}')
            (root/'plugins/registry.json').write_text('runtime registry')
            (root/'plugins/custom').mkdir()
            (root/'plugins/custom/index.js').write_text('locally upgraded')
            self.assertEqual(first, fingerprint())
            (root/'plugins/surface.json').write_text('{"generatedAt":"after", "plugins":[]}')
            self.assertEqual(first, fingerprint())
            (root/'internal/file.go').write_text('new host implementation')
            self.assertNotEqual(first, fingerprint())
            self.assertIn('plugins/surface.json', (ROOT/'.dockerignore').read_text().splitlines())

if __name__ == '__main__':
    unittest.main()
