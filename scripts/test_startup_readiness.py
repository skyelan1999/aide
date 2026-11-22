#!/usr/bin/env python3
"""Exercise readiness deadlines with a simulated clock and Docker/HTTPS probes."""
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
PROBE = r'''
import json, os, sys
from pathlib import Path
a=sys.argv[1:]; root=Path(os.environ['FIXTURE_ROOT'])
with (root/'calls').open('a') as f: f.write(json.dumps([Path(sys.argv[0]).name,*a])+'\n')
def count(name):
 p=root/name; n=int(p.read_text())+1 if p.exists() else 1; p.write_text(str(n)); return n
if Path(sys.argv[0]).name=='curl':
 if count('host-count')<=int(os.environ.get('HOST_FAILS','0')):sys.exit(28)
 print(json.dumps({'service':'aide','status':'ok','instanceId':os.environ.get('HOST_INSTANCE','fixture')}));sys.exit(0)
if a[:3]==['compose','ps','--all']:print('a'*64);sys.exit(0)
if a[:2]==['inspect','--format']:
 exited=os.environ.get('EXIT_AFTER_PROBE')=='1' and (root/'container-count').exists()
 print(('exited' if exited else 'running')+'|'+str(root)+'|aide');sys.exit(0)
if a[:3]==['compose','exec','-T']:
 if count('container-count')<=int(os.environ.get('CONTAINER_FAILS','0')):sys.exit(7)
 print(json.dumps({'service':'aide','status':'ok','instanceId':'fixture'}));sys.exit(0)
sys.exit(91)
'''


class ReadinessTests(unittest.TestCase):
    def run_probe(self, **options):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('docker', 'curl'):
                script = root / name
                script.write_text('#!' + sys.executable + '\n' + PROBE)
                script.chmod(0o755)
            env = dict(os.environ, FIXTURE_ROOT=str(root), PATH=str(root)+os.pathsep+os.environ['PATH'])
            env.pop('AIDE_STARTUP_TIMEOUT', None)
            env.update({k: str(v) for k, v in options.items()})
            result = subprocess.run(['bash', '-ec', r'''
source "$1"
PROJECT_DIR="$FIXTURE_ROOT"
sleep(){ SECONDS=$((SECONDS + 15)); }
SECONDS=0
launcher_wait_container_ready docker
launcher_wait_host_ready 9999 /fixture/cert fixture
''', 'fixture', str(ROOT / 'scripts/prebuilt-image.sh')],
                                    env=env, capture_output=True, text=True, timeout=10)
            calls = [json.loads(s) for s in (root/'calls').read_text().splitlines()] if (root/'calls').exists() else []
            return result, calls

    def test_slow_container_recovers_after_old_sixty_second_limit(self):
        result, calls = self.run_probe(CONTAINER_FAILS=8)
        self.assertEqual(result.returncode, 0, result.stderr)
        elapsed = [int(s) for s in re.findall(r'\((\d+)/600', result.stderr.replace('（', '('))]
        self.assertGreaterEqual(max(elapsed), 120)
        probes = [a for a in calls if a[:4] == ['docker','compose','exec','-T']]
        self.assertEqual(len(probes), 9)
        self.assertTrue(all('--max-time' in a for a in probes))

    def test_host_tls_can_become_ready_later_without_manual_retry(self):
        result, calls = self.run_probe(HOST_FAILS=6)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len([a for a in calls if a[0]=='curl']), 7)
        self.assertTrue(all('--cacert' in a and '--noproxy' in a for a in calls if a[0]=='curl'))

    def test_container_and_host_share_the_same_deadline(self):
        result, calls = self.run_probe(CONTAINER_FAILS=2, HOST_FAILS=9, AIDE_STARTUP_TIMEOUT=45)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('45', result.stderr)
        self.assertEqual(len([a for a in calls if a[0]=='curl']), 1)

    def test_permanent_failure_is_bounded_and_does_not_mutate_container(self):
        result, calls = self.run_probe(CONTAINER_FAILS=99, AIDE_STARTUP_TIMEOUT=30)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len([a for a in calls if a[:4]==['docker','compose','exec','-T']]), 2)
        self.assertFalse(any(x in a for a in calls for x in ('up','stop','rm','restart','pull')))

    def test_exited_container_fails_promptly(self):
        result, calls = self.run_probe(CONTAINER_FAILS=99, EXIT_AFTER_PROBE=1)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len([a for a in calls if a[:4]==['docker','compose','exec','-T']]), 1)

    def test_wrong_host_identity_is_never_accepted(self):
        result, calls = self.run_probe(HOST_INSTANCE='foreign')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len([a for a in calls if a[0]=='curl']), 1)

    def test_invalid_budget_is_rejected_before_probing(self):
        for value in ('0','1801','-1','1;echo bad','ten'):
            with self.subTest(value=value):
                result, calls = self.run_probe(AIDE_STARTUP_TIMEOUT=value)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])


if __name__ == '__main__':
    unittest.main()
