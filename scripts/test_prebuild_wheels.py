#!/usr/bin/env python3
"""Wheelhouse replacement lifecycle using fake Docker and owned build inputs."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class WheelhouseReplacementTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="aide-wheelhouse-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "scripts").mkdir()
        (self.root / "bin").mkdir()
        self.wheels = self.root / "docker/wheels"
        self.wheels.mkdir(parents=True)
        shutil.copy(ROOT / "scripts/prebuild-wheels.sh", self.root / "scripts/prebuild-wheels.sh")
        (self.wheels / ".gitkeep").write_text("owned marker")
        (self.wheels / "old-1-py3-none-any.whl").write_bytes(b"owned last known good")
        self.write("bin/docker", """#!/usr/bin/env python3
import json,os,pathlib,sys
root=pathlib.Path(os.environ['WHEEL_FIXTURE_ROOT']);args=sys.argv[1:]
with (root/'calls').open('a') as log: log.write(json.dumps(args)+'\\n')
if args[0]=='image': sys.exit(0)
assert args[0]=='run'
mount=pathlib.Path(args[args.index('-v')+1].split(':',1)[0])
mode=os.environ['WHEEL_FIXTURE_MODE']
if 'download' in args:
    (root/'live-during-download').write_text(str((root/'docker/wheels/old-1-py3-none-any.whl').exists()))
    if mode!='empty-success': (mount/'new-2-py3-none-any.whl').write_bytes(b'owned replacement')
    if mode=='download-failure': sys.exit(17)
elif 'install' in args and '--dry-run' in args:
    (root/'live-during-verification').write_text(str((root/'docker/wheels/old-1-py3-none-any.whl').exists()))
    if mode=='verification-failure' or not list(mount.glob('*.whl')): sys.exit(18)
else: raise AssertionError(args)
""")
        self.write("bin/mv", """#!/usr/bin/env python3
import os,sys
if os.environ['WHEEL_FIXTURE_MODE']=='promotion-failure' and sys.argv[-2].endswith('/next'):
    sys.exit(19)
os.execv(os.environ['WHEEL_FIXTURE_REAL_MV'],['mv']+sys.argv[1:])
""")

    def write(self, relative, text):
        target = self.root / relative
        target.write_text(text)
        target.chmod(0o700)

    def run_prebuild(self, mode):
        env = dict(os.environ, PATH=str(self.root / "bin") + os.pathsep + os.environ["PATH"],
                   WHEEL_FIXTURE_ROOT=str(self.root), WHEEL_FIXTURE_MODE=mode,
                   WHEEL_FIXTURE_REAL_MV=shutil.which("mv"), LC_ALL="C")
        return subprocess.run(["bash", "scripts/prebuild-wheels.sh"], cwd=self.root, env=env,
                              capture_output=True, text=True, timeout=15)

    def assert_old_intact(self):
        self.assertEqual((self.wheels / "old-1-py3-none-any.whl").read_bytes(), b"owned last known good")
        self.assertEqual((self.wheels / ".gitkeep").read_text(), "owned marker")
        self.assertEqual(sorted(p.name for p in self.wheels.iterdir()), [".gitkeep", "old-1-py3-none-any.whl"])

    def test_failed_download_preserves_last_known_good(self):
        result = self.run_prebuild("download-failure")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_old_intact()

    def test_failed_resolver_verification_preserves_last_known_good(self):
        result = self.run_prebuild("verification-failure")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_old_intact()

    def test_success_replaces_only_after_download_and_verification(self):
        result = self.run_prebuild("normal")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "live-during-download").read_text(), "True")
        self.assertEqual((self.root / "live-during-verification").read_text(), "True")
        self.assertEqual(sorted(p.name for p in self.wheels.iterdir()), [".gitkeep", "new-2-py3-none-any.whl"])
        self.assertFalse(list((self.root / "docker").glob(".wheelhouse-*")))
        calls = [json.loads(line) for line in (self.root / "calls").read_text().splitlines()]
        runs = [args for args in calls if args[0] == "run"]
        self.assertEqual(len(runs), 2)
        for args in runs:
            self.assertEqual(args[args.index("--user") + 1], f"{os.getuid()}:{os.getgid()}")
        self.assertIn("--ignore-installed", runs[1])
        self.assertIn("--no-index", runs[1])

    def test_empty_success_rejected(self):
        result = self.run_prebuild("empty-success")
        self.assertNotEqual(result.returncode, 0)
        self.assert_old_intact()

    def test_failed_promotion_restores_previous_directory(self):
        result = self.run_prebuild("promotion-failure")
        self.assertNotEqual(result.returncode, 0)
        self.assert_old_intact()

    def test_existing_lock_refuses_other_run_without_touching_inputs(self):
        (self.root / "docker/.wheelhouse-lock").mkdir()
        result = self.run_prebuild("normal")
        self.assertNotEqual(result.returncode, 0)
        self.assert_old_intact()
        self.assertTrue((self.root / "docker/.wheelhouse-lock").is_dir())


if __name__ == "__main__":
    unittest.main()
