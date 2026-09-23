"""Exercise installer orchestration in a temporary root with a fake Docker CLI."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]

class InstallTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root/'scripts').mkdir()
        shutil.copy(ROOT/'scripts/install.sh', self.root/'scripts/install.sh')
        shutil.copy(ROOT/'.env.example', self.root/'.env.example')
        (self.root/'scripts/version.sh').write_text('echo "0.1.6.0 RC1"\n')
        (self.root/'scripts/aide.sh').write_text('echo "$1" >> actions\n')
        (self.root/'docker').write_text('''#!/bin/bash
printf '%s\\n' "$*" >> "$CALL_LOG"
case "$*" in
  'info --format '{{.Architecture}}) echo aarch64 ;;
  'image inspect '*) echo arm64 ;;
esac
exit 0
''')
        (self.root/'docker').chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root)+os.pathsep+os.environ['PATH'], CALL_LOG=str(self.root/'calls'))
    def run_install(self, *args):
        return subprocess.run(['bash','scripts/install.sh',*args], cwd=self.root,env=self.env,capture_output=True,text=True)
    def archive(self, valid=True):
        p=self.root/'image.tar.gz';p.write_bytes(b'fixture')
        (self.root/'SHA256SUMS').write_text((hashlib.sha256(p.read_bytes()).hexdigest() if valid else '0'*64)+'  image.tar.gz\n')
        return str(p)
    def test_check_does_not_mutate_configuration(self):
        r=self.run_install('--check');self.assertEqual(r.returncode,0,r.stderr)
        self.assertFalse((self.root/'.env').exists());self.assertFalse((self.root/'actions').exists())
    def test_source_keeps_existing_configuration(self):
        p=self.root/'.env';p.write_text('PRIVATE_SETTING=preserve\n')
        r=self.run_install('--source');self.assertEqual(r.returncode,0,r.stderr)
        self.assertEqual(p.read_text(),'PRIVATE_SETTING=preserve\n')
        self.assertEqual((self.root/'actions').read_text(),'start\n')
    def test_image_initializes_versioned_configuration(self):
        r=self.run_install('--image',self.archive());self.assertEqual(r.returncode,0,r.stderr)
        self.assertIn('AIDE_IMAGE=aide:0.1.6.0-RC1',(self.root/'.env').read_text())
        self.assertIn('AIDE_CONTEXT=./context',(self.root/'.env').read_text())
        self.assertEqual((self.root/'actions').read_text(),'start-image\n')
    def test_bad_checksum_never_loads_or_starts(self):
        r=self.run_install('--image',self.archive(False));self.assertNotEqual(r.returncode,0)
        self.assertNotIn('image load',(self.root/'calls').read_text())
        self.assertFalse((self.root/'actions').exists())

if __name__=='__main__': unittest.main()
