"""Source identity stays stable across generated plugin catalogue refreshes."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]

class SourceFingerprintTests(unittest.TestCase):
    def test_generated_surface_is_excluded_but_plugin_code_is_included(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for name in ['cmd', 'internal', 'plugins/custom', 'vendor', 'scripts/office', 'docker/wheels', 'docker/sherpa']:
                (root/name).mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT/'scripts/aide.sh', root/'scripts/aide.sh')
            for name in ['go.mod', 'go.sum', 'Dockerfile', 'compose.yaml', '.dockerignore', 'version.md']:
                (root/name).write_text(name)
            (root/'internal/file.go').write_text('package fixture\n')
            (root/'plugins/custom/index.js').write_text('fixture')
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
            self.assertEqual(first, fingerprint())
            (root/'plugins/surface.json').write_text('{"generatedAt":"after", "plugins":[]}')
            self.assertEqual(first, fingerprint())
            (root/'plugins/custom/index.js').write_text('new implementation')
            self.assertNotEqual(first, fingerprint())
            self.assertIn('plugins/surface.json', (ROOT/'.dockerignore').read_text().splitlines())

if __name__ == '__main__':
    unittest.main()
