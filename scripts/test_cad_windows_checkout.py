#!/usr/bin/env python3
"""Reproduce Windows CRLF shell failure with mocked installers, no downloads."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]

class CadWindowsCheckoutTests(unittest.TestCase):
    def test_crlf_installer_and_build_normalization(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            commands = root / 'bin'
            commands.mkdir()
            for name in ('apt-get', 'python3', 'ldd', 'chmod', 'rm'):
                stub = commands / name
                stub.write_text('#!/bin/sh\nexit 0\n')
                stub.chmod(0o755)
            script = root / 'install.sh'
            source = (ROOT / 'docker/cad/install.sh').read_text().replace('/opt/aide-cad', str(root / 'cad'))
            script.write_bytes(source.replace('\n', '\r\n').encode())
            env = dict(os.environ, PATH=str(commands) + os.pathsep + os.environ['PATH'])
            broken = subprocess.run(['sh', str(script)], env=env, capture_output=True)
            self.assertNotEqual(broken.returncode, 0, 'CRLF fixture did not reproduce shell failure')
            script.write_bytes(script.read_bytes().replace(b'\r\n', b'\n'))
            fixed = subprocess.run(['sh', str(script)], env=env, capture_output=True)
            self.assertEqual(fixed.returncode, 0, fixed.stderr.decode())
        for name in ('Dockerfile', 'docker/Dockerfile.cad'):
            self.assertIn("RUN sed -i 's/\\r$//' /cad-install.sh /cad-requirements.txt && sh /cad-install.sh", (ROOT / name).read_text())

    def test_git_checkout_forces_container_scripts_to_lf(self):
        result = subprocess.check_output(['git', 'check-attr', 'eol', '--', 'docker/cad/install.sh', 'scripts/aide.sh', 'docker/cad/requirements.txt'], cwd=ROOT, text=True)
        self.assertEqual(result.count(': eol: lf'), 3)

if __name__ == '__main__':
    unittest.main()
