"""Execute the real POSIX download recovery wrapper at a disposable pip boundary."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]


class PipRetryTests(unittest.TestCase):
    def run_case(self, failures):
        with tempfile.TemporaryDirectory(prefix='aide-pip-retry-') as temporary:
            base=Path(temporary)
            command=base/'pip-fixture'
            command.write_text('#!'+sys.executable+'\n'+r'''
import json,os,pathlib,sys
p=pathlib.Path(os.environ['ATTEMPT_LOG'])
entries=json.loads(p.read_text()) if p.exists() else []
entries.append(sys.argv[1:]);p.write_text(json.dumps(entries))
sys.exit(23 if len(entries)<=int(os.environ['FAILURES']) else 0)
''')
            command.chmod(0o700)
            pause=base/'sleep';pause.write_text('#!/bin/sh\nexit 0\n');pause.chmod(0o700)
            arguments=['install','--timeout','60','-i','https://explicit.example/simple/','python-docx==1.2.0','pypdf[crypto]==6.19.0']
            result=subprocess.run(['sh',str(ROOT/'scripts/pip-install-retry.sh'),str(command),*arguments],
                env=dict(os.environ,PATH=str(base)+os.pathsep+os.environ['PATH'],ATTEMPT_LOG=str(base/'calls.json'),FAILURES=str(failures)),
                capture_output=True,text=True,timeout=10)
            return result,json.loads((base/'calls.json').read_text()),arguments

    def test_first_success_is_not_repeated(self):
        result,calls,arguments=self.run_case(0)
        self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(calls,[arguments])

    def test_partial_download_failure_retries_whole_install_without_changing_index_or_pins(self):
        result,calls,arguments=self.run_case(2)
        self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(calls,[arguments]*3)

    def test_permanent_failure_is_bounded_and_preserves_exit_status(self):
        result,calls,arguments=self.run_case(9)
        self.assertEqual(result.returncode,23);self.assertEqual(calls,[arguments]*3)
        self.assertIn('PIP_INDEX_URL',result.stderr)


if __name__=='__main__':unittest.main()
