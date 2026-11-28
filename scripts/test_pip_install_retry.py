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
            command.write_text('#!/bin/sh\n""":"\nexec "'+sys.executable+'" "$0" "$@"\n":"""\n'+r'''
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

    def test_private_output_cache_and_index_are_isolated_on_success_and_failure(self):
        with tempfile.TemporaryDirectory(prefix='aide-private-index-') as temporary:
            base=Path(temporary)
            secret=base/'pip-index'
            secret.write_text('https://fixture:sentinel@private.invalid/simple/\n')
            wrapper=base/'wrapper'
            wrapper.write_text((ROOT/'scripts/pip-install-retry.sh').read_text().replace(
                'secret_file=/run/secrets/aide_pip_index', 'secret_file='+str(secret)))
            pause=base/'sleep';pause.write_text('#!/bin/sh\nexit 0\n');pause.chmod(0o700)
            fixture=base/'fixture.py'
            fixture.write_text("""import os, sys
with open(os.environ['RECORD'], 'a') as log:
    print(os.environ['PIP_INDEX_URL'], os.environ['PIP_NO_CACHE_DIR'],
          os.environ['PIP_CONFIG_FILE'], repr(os.environ['PIP_EXTRA_INDEX_URL']),
          repr(sys.argv[1:]), file=log)
print(os.environ['PIP_INDEX_URL'], file=sys.stderr)
sys.exit(int(os.environ['RESULT']))
""")
            for status,count in ((0,1),(23,3)):
                record=base/f'attempts-{status}'
                result=subprocess.run(['sh',str(wrapper),sys.executable,str(fixture),'install','demo==1'],
                    env=dict(os.environ,PATH=str(base)+os.pathsep+os.environ['PATH'],RECORD=str(record),
                             RESULT=str(status),AIDE_PRIVATE_PIP='1',PIP_EXTRA_INDEX_URL='https://public.invalid'),
                    capture_output=True,text=True,timeout=10)
                self.assertEqual(result.returncode,status)
                self.assertNotIn('sentinel', result.stdout+result.stderr)
                self.assertEqual(record.read_text().splitlines(),
                    ["https://fixture:sentinel@private.invalid/simple/ 1 /dev/null '' ['install', 'demo==1']"]*count)
            secret.unlink()
            result=subprocess.run(['sh',str(wrapper),'false'],env=dict(os.environ,AIDE_PRIVATE_PIP='1'),
                                  capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,2)
            self.assertIn('missing',result.stderr)

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
