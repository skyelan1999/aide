#!/usr/bin/env python3
"""Exercise release smoke ownership with fake Docker in an owned directory."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CID = "a" * 64


class ReleaseSmokeOwnershipTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="aide-release-ownership-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "scripts").mkdir()
        (self.root / "bin").mkdir()
        shutil.copy(ROOT / "scripts/docker-release.sh", self.root / "scripts/docker-release.sh")
        self.write("scripts/version.sh", "#!/bin/sh\nprintf '0.0.0.1\\n'\n")
        self.write("scripts/aide.sh", "#!/bin/sh\nprintf 'owned-source\\n'\n")
        self.write("bin/git", "#!/bin/sh\nprintf 'owned-commit\\n'\n")
        self.write("bin/sleep", "#!/bin/sh\nexit 0\n")
        self.write("bin/curl", """#!/usr/bin/env python3
import os,sys
if os.environ['RELEASE_FIXTURE_MODE']=='health-failure': sys.exit(22)
print('{"status":"ok","service":"aide"}')
""")
        self.write("bin/docker", """#!/usr/bin/env python3
import json,os,pathlib,sys
root=pathlib.Path(os.environ['RELEASE_FIXTURE_ROOT']);args=sys.argv[1:]
with (root/'calls').open('a') as f: f.write(json.dumps(args)+'\\n')
mode=os.environ['RELEASE_FIXTURE_MODE'];cid='a'*64
if args[0]=='run':
    if mode=='name-collision': sys.exit(125)
    if mode=='invalid-cid': cid='unrelated-container-name'
    if '--cidfile' in args:
        pathlib.Path(args[args.index('--cidfile')+1]).write_text(cid+'\\n')
    print(cid)
    if mode=='run-failure-after-create': sys.exit(125)
elif args[0]=='cp': pathlib.Path(args[-1]).write_text('owned-certificate')
elif args[0]=='save': sys.stdout.buffer.write(b'owned-fake-image')
elif args[0]=='rm' and 'aide-rel-test' in args:
    (root/'foreign-container').unlink(missing_ok=True)
""")
        (self.root / "foreign-container").write_text("unrelated-running-workload")

    def write(self, rel, text):
        target = self.root / rel
        target.write_text(text)
        target.chmod(0o700)

    def run_release(self, mode="normal"):
        env = dict(os.environ, PATH=str(self.root / "bin") + os.pathsep + os.environ["PATH"],
                   RELEASE_FIXTURE_ROOT=str(self.root), RELEASE_FIXTURE_MODE=mode)
        result = subprocess.run(["bash", "scripts/docker-release.sh"], cwd=self.root, env=env,
                                capture_output=True, text=True, timeout=15)
        calls = [json.loads(line) for line in (self.root / "calls").read_text().splitlines()]
        return result, calls

    def assert_foreign_untouched(self, calls):
        self.assertTrue((self.root / "foreign-container").is_file(), "release removed unrelated aide-rel-test")
        for args in calls:
            if args[0] in ("rm", "stop"):
                self.assertEqual(args[-1], CID, "cleanup/stop must address only this run's owned CID")

    def test_success_preserves_existing_container_and_cleans_owned_id(self):
        result, calls = self.run_release()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_foreign_untouched(calls)
        self.assertIn(["rm", "-f", CID], calls)
        self.assertTrue(any(args[0] == "save" for args in calls))

    def test_health_failure_preserves_foreign_and_never_exports(self):
        result, calls = self.run_release("health-failure")
        self.assertNotEqual(result.returncode, 0)
        self.assert_foreign_untouched(calls)
        self.assertIn(["rm", "-f", CID], calls)
        self.assertFalse(any(args[0] == "save" for args in calls))

    def test_name_collision_has_no_owned_container_to_remove(self):
        result, calls = self.run_release("name-collision")
        self.assertNotEqual(result.returncode, 0)
        self.assert_foreign_untouched(calls)
        self.assertFalse(any(args[0] in ("rm", "stop", "save") for args in calls))

    def test_failed_run_with_cid_still_cleans_only_owned_container(self):
        result, calls = self.run_release("run-failure-after-create")
        self.assertNotEqual(result.returncode, 0)
        self.assert_foreign_untouched(calls)
        self.assertIn(["rm", "-f", CID], calls)
        self.assertFalse(any(args[0] == "save" for args in calls))

    def test_invalid_cid_never_becomes_a_cleanup_target(self):
        result, calls = self.run_release("invalid-cid")
        self.assertNotEqual(result.returncode, 0)
        self.assert_foreign_untouched(calls)
        self.assertFalse(any(args[0] in ("rm", "stop", "save") for args in calls))


if __name__ == "__main__":
    unittest.main()
