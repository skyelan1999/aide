"""Exercise offline bootstrap without using the user's Docker engine or data."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
IMAGE_ID = 'sha256:' + 'a' * 64


class OfflineBundleTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='aide bundle ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for name in ('scripts', 'docker-images', 'bin'):
            (self.root / name).mkdir()
        shutil.copy(ROOT / 'start.command', self.root)
        shutil.copy(ROOT / 'scripts/aide.sh', self.root / 'scripts')
        shutil.copy(ROOT / 'docker/offline.env.example', self.root / '.env.example')
        (self.root / '.aide-image').write_text(f'aide:offline-aaaa {IMAGE_ID} linux/arm64\n')
        archive = self.root / 'docker-images/aide-local.tar'
        archive.write_bytes(b'image fixture')
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (archive.parent / 'SHA256SUMS').write_text(f'{digest}  aide-local.tar\n')
        docker = self.root / 'bin/docker'
        docker.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
a=sys.argv[1:]; root=pathlib.Path(os.environ['FIXTURE_ROOT'])
with (root/'calls').open('a') as f: f.write(json.dumps(a)+'\\n')
if a == ['info']: pass
elif a[:2] == ['info','--format']: print(os.environ.get('ENGINE_PLATFORM','linux/aarch64'))
elif a[:2] == ['image','inspect']:
    if not (root/'image-id').exists(): sys.exit(1)
    print((root/'image-id').read_text())
elif a[:2] == ['image','load']:
    (root/'image-id').write_text(os.environ.get('LOADED_ID',os.environ['EXPECTED_ID']))
elif a[:3] == ['compose','port','aide']: print('127.0.0.1:18097')
elif a[:2] == ['compose','up']:
    assert '--no-build' in a and '--pull' in a and a[a.index('--pull')+1] == 'never'
elif a[:2] in (['compose','exec'], ['compose','stop']): pass
else: sys.exit('Unexpected docker operation: '+repr(a))
''')
        docker.chmod(0o755)
        self.env = dict(os.environ, PATH=str(docker.parent) + os.pathsep + os.environ['PATH'],
                        FIXTURE_ROOT=str(self.root), EXPECTED_ID=IMAGE_ID, AIDE_OPEN_BROWSER='0')

    def run_start(self):
        return subprocess.run(['bash', 'start.command'], cwd=self.root, env=self.env,
                              capture_output=True, text=True)

    def calls(self):
        return (self.root / 'calls').read_text()

    def test_first_start_loads_checked_image_without_building(self):
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('"image", "load"', self.calls())
        self.assertIn('"--no-build", "--pull", "never"', self.calls())
        self.assertTrue((self.root / '.env').exists())

    def test_repeat_start_keeps_configuration_and_skips_load(self):
        (self.root / 'image-id').write_text(IMAGE_ID)
        (self.root / '.env').write_text('AIDE_PORT=19097\n')
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('"image", "load"', self.calls())
        self.assertEqual((self.root / '.env').read_text(), 'AIDE_PORT=19097\n')

    def test_architecture_mismatch_stops_before_loading(self):
        self.env['ENGINE_PLATFORM'] = 'linux/x86_64'
        result = self.run_start()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('架构', result.stderr)
        self.assertNotIn('"load"', self.calls())
        self.assertNotIn('"compose"', self.calls())

    def test_corrupt_archive_never_loads(self):
        (self.root / 'docker-images/aide-local.tar').write_bytes(b'corrupt')
        self.assertNotEqual(self.run_start().returncode, 0)
        self.assertNotIn('"load"', self.calls())
        self.assertNotIn('"compose"', self.calls())

    def test_wrong_loaded_identity_never_starts(self):
        self.env['LOADED_ID'] = 'sha256:' + 'b' * 64
        self.assertNotEqual(self.run_start().returncode, 0)
        self.assertNotIn('"compose"', self.calls())


if __name__ == '__main__':
    unittest.main()
