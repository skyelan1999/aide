#!/usr/bin/env python3
"""Exercise bridge launchers on the system Bash without touching real bridges."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class BridgeLauncherTests(unittest.TestCase):
    def exercise(self, kind, pid_state):
        with tempfile.TemporaryDirectory(prefix="aide-bridge-launcher-") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / ".cache").mkdir()
            (root / "bin").mkdir()
            script = root / "scripts" / f"{kind}-bridge-control.sh"
            shutil.copyfile(ROOT / "scripts" / script.name, script)
            token_name = "BROWSER" if kind == "safari" else "COMPUTER"
            (root / ".env").write_text(f"AIDE_{token_name}_BRIDGE_TOKEN={'x' * 40}\n")
            # Stub only external commands; builtin kill still checks actual PIDs.
            mocks = {
                "uname": "#!/bin/sh\necho Darwin\n",
                "ps": f"#!/bin/sh\necho node scripts/{kind}-bridge.js\n",
                "node": '#!/bin/sh\nif [ "$1" = "-e" ]; then exit 0; fi\nexec sleep 30\n',
            }
            for name, contents in mocks.items():
                target = root / "bin" / name
                target.write_text(contents)
                target.chmod(0o755)
            env = dict(os.environ, PATH=str(root / "bin") + ":" + os.environ["PATH"], AIDE_NATIVE_BRIDGE_APP=str(root / "not-installed.app"))
            pid_file = root / ".cache" / f"aide-{kind}-bridge.pid"
            if pid_state == "live":
                pid_file.write_text(str(os.getpid()))
            elif pid_state == "stale":
                pid_file.write_text("not-a-pid")
            elif pid_state == "empty":
                pid_file.write_text("")
            child_pid = None
            try:
                result = subprocess.run(["/bin/bash", str(script), "start"], env=env,
                                        capture_output=True, timeout=15)
                self.assertEqual(result.returncode, 0, result.stderr.decode(errors="replace"))
                self.assertNotIn(b"unbound variable", result.stderr)
                if pid_state == "live":
                    self.assertEqual(pid_file.read_text(), str(os.getpid()))
                    self.assertIn(str(os.getpid()).encode(), result.stdout)
                    return
                child_pid = int(pid_file.read_text())
                repeated = subprocess.run(["/bin/bash", str(script), "start"], env=env,
                                          capture_output=True, timeout=15)
                self.assertEqual(repeated.returncode, 0, repeated.stderr.decode(errors="replace"))
                self.assertEqual(int(pid_file.read_text()), child_pid)
                stopped = subprocess.run(["/bin/bash", str(script), "stop"], env=env,
                                         capture_output=True, timeout=15)
                self.assertEqual(stopped.returncode, 0)
                self.assertFalse(pid_file.exists())
            finally:
                if child_pid:
                    try:
                        os.kill(child_pid, 15)
                    except ProcessLookupError:
                        pass

    def test_start_repeat_and_stop(self):
        for kind in ("safari", "computer"):
            for state in ("live", "stale", "empty", "missing"):
                with self.subTest(kind=kind, pid_state=state):
                    self.exercise(kind, state)


if __name__ == "__main__":
    unittest.main()
