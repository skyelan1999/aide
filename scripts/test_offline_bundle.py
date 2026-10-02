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
elif a[:1] == ['ps']: pass
elif a[:2] == ['info','--format']: print(os.environ.get('ENGINE_PLATFORM','linux/aarch64'))
elif a[:2] == ['image','inspect']:
    if not (root/'image-id').exists(): sys.exit(1)
    print((root/'image-id').read_text())
elif a[:2] == ['image','load']:
    (root/'image-id').write_text(os.environ.get('LOADED_ID',os.environ['EXPECTED_ID']))
elif a[:3] == ['compose','port','aide']: print('127.0.0.1:18097')
elif a[:2] == ['compose','up']:
    assert '--no-build' in a and '--pull' in a and a[a.index('--pull')+1] == 'never'
elif a[:2] in (['compose','ps'], ['compose','run']): pass
elif a[:2] in (['compose','exec'], ['compose','stop']): pass
else: sys.exit('Unexpected docker operation: '+repr(a))
''')
        docker.chmod(0o755)
        opener = self.root / 'bin/open'
        opener.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$FIXTURE_ROOT/opened-url"\n')
        opener.chmod(0o755)
        self.env = dict(os.environ, PATH=str(docker.parent) + os.pathsep + os.environ['PATH'],
                        FIXTURE_ROOT=str(self.root), EXPECTED_ID=IMAGE_ID, AIDE_OPEN_BROWSER='0', AIDE_PORT='18097')

    def run_start(self):
        return subprocess.run(['bash', 'start.command'], cwd=self.root, env=self.env,
                              capture_output=True, text=True, errors='replace')

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

    def test_release_image_download_resumes_after_interruption(self):
        tag = 'v0.1.14.0-RC99'
        archive_name = f'aide-{tag}-linux-arm64-image.tar.gz'
        payload = b'release image fixture for resumable download'
        expected = hashlib.sha256(payload).hexdigest()
        (self.root / 'docker-images/aide-local.tar').unlink()
        (self.root / 'docker-images/SHA256SUMS').unlink()
        (self.root / '.aide-image').write_text(f'aide:offline-aaaa {IMAGE_ID} linux/arm64 {tag}\n')
        (self.root / 'image-payload').write_bytes(payload)
        curl = self.root / 'bin/curl'
        curl.write_text('''#!/usr/bin/env python3
import os, pathlib, sys
a=sys.argv[1:]; root=pathlib.Path(os.environ['FIXTURE_ROOT'])
out=pathlib.Path(a[a.index('--output')+1]); url=a[-1]
if url.endswith('/SHA256SUMS'):
    name=os.environ['ARCHIVE_NAME']; digest=os.environ['ARCHIVE_HASH']
    out.write_text(f'{digest}  {name}\\n')
    sys.exit(0)
payload=(root/'image-payload').read_bytes(); calls=root/'curl-image-calls'
count=int(calls.read_text())+1 if calls.exists() else 1; calls.write_text(str(count))
if count == 1:
    out.write_bytes(payload[:len(payload)//2])
    sys.exit(18)
assert '--continue-at' in a and a[a.index('--continue-at')+1] == '-'
with out.open('ab') as f: f.write(payload[len(out.read_bytes()):])
''')
        curl.chmod(0o755)
        self.env.update(ARCHIVE_NAME=archive_name, ARCHIVE_HASH=expected)
        self.env['AIDE_OPEN_BROWSER'] = '1'
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertEqual((self.root / 'docker-images' / archive_name).read_bytes(), payload)
        self.assertEqual((self.root / 'curl-image-calls').read_text(), '2')
        self.assertIn('"image", "load"', self.calls())
        self.assertEqual((self.root / 'opened-url').read_text().strip(), 'https://localhost:18097/')


if __name__ == '__main__':
    unittest.main()
