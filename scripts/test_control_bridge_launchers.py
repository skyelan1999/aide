#!/usr/bin/env python3
"""Exercise bridge launchers on the system Bash without touching real bridges."""
import os
import http.server
import json
import re
import threading
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
            shutil.copyfile(ROOT / "scripts/bridge-lifecycle-lock.py", root / "scripts/bridge-lifecycle-lock.py")
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

    def test_concurrent_election_keeps_one_receipt(self):
        with tempfile.TemporaryDirectory(prefix='aide-bridge-election-') as directory:
            root=Path(directory);(root/'scripts').mkdir();(root/'bin').mkdir();(root/'.cache').mkdir()
            for name in ('safari-bridge-control.sh','bridge-lifecycle-lock.py'):
                shutil.copyfile(ROOT/'scripts'/name,root/'scripts'/name)
            (root/'.env').write_text('AIDE_BROWSER_BRIDGE_TOKEN='+'x'*40+'\n')
            for name,body in {
                'uname':'#!/bin/sh\necho Darwin\n',
                'ps':'#!/bin/sh\necho node scripts/safari-bridge.js\n',
                'node':'#!/bin/sh\nif [ "$1" = -e ]; then sleep .05;exit 0;fi\nprintf spawned\\n >> "$BRIDGE_TEST_SPAWNS"\nexec sleep 30\n'
            }.items():
                p=root/'bin'/name;p.write_text(body);p.chmod(0o700)
            env={**os.environ,'PATH':str(root/'bin')+os.pathsep+os.environ['PATH'],'BRIDGE_TEST_SPAWNS':str(root/'spawns')}
            script=str(root/'scripts/safari-bridge-control.sh');pid_file=root/'.cache/aide-safari-bridge.pid';pid=None
            try:
                children=[subprocess.Popen(['/bin/bash',script,'start'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE) for _ in range(8)]
                for child in children:
                    out,err=child.communicate(timeout=15);self.assertEqual(child.returncode,0,err)
                pid=int(pid_file.read_text())
                self.assertEqual((root/'spawns').read_text().count('spawned'),1)
                self.assertTrue(Path(str(pid_file)+'.lock').exists())
                result=subprocess.run(['/bin/bash',script,'stop'],env=env,capture_output=True,timeout=15)
                self.assertEqual(result.returncode,0,result.stderr);self.assertFalse(pid_file.exists())
            finally:
                if pid:
                    try:os.kill(pid,15)
                    except ProcessLookupError:pass

    def test_readiness_must_belong_to_spawned_pid(self):
        class Handler(http.server.BaseHTTPRequestHandler):
            peer_pid=12345
            service='fixture'
            def do_GET(self):
                self.send_response(200);self.end_headers()
                self.wfile.write(json.dumps({'ok':True,'pid':self.peer_pid,'service':self.service}).encode())
            def log_message(self,*args):pass
        server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            for name in ('safari-bridge-control.sh','computer-bridge-control.sh','headless-browser-control.sh'):
                source=(ROOT/'scripts'/name).read_text()
                for program in re.findall(r"node -e '([^']+)'",source):
                    if '/healthz' not in program or 'EXPECTED_PID' not in program:continue
                    service=re.search(r'x.service==="([^"]+)"',program)
                    Handler.service=service.group(1) if service else 'fixture'
                    code=re.sub(r'http://127\.0\.0\.1:[0-9]+/healthz','http://127.0.0.1:'+str(server.server_port)+'/healthz',program)
                    for actual,success in [(12345,True),(12346,False)]:
                        Handler.peer_pid=actual
                        result=subprocess.run(['node','-e',code],env={**os.environ,'EXPECTED_PID':'12345','TOKEN':'x'*40},capture_output=True,timeout=5)
                        self.assertEqual(result.returncode==0,success,name+': '+result.stderr.decode())
        finally:
            server.shutdown();server.server_close();thread.join(timeout=2)

    def test_start_repeat_and_stop(self):
        for kind in ("safari", "computer"):
            for state in ("live", "stale", "empty", "missing"):
                with self.subTest(kind=kind, pid_state=state):
                    self.exercise(kind, state)


if __name__ == "__main__":
    unittest.main()
