"""Exercise local-only dependency selection and checksum handling with fake Docker."""
import os,json,shutil,subprocess,tempfile,unittest,hashlib
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class DependencyImageTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='aide dependency ');self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
  for d in ['scripts','docker','docker-images','bin']:(self.root/d).mkdir()
  shutil.copy(ROOT/'scripts/prepare-local-base.sh',self.root/'scripts');shutil.copy(ROOT/'docker/Dockerfile.local-base',self.root/'docker')
  f=self.root/'bin/docker';f.write_text('''#!/usr/bin/env python3
import json,os,sys,pathlib
a=sys.argv[1:];r=pathlib.Path(os.environ['FIXTURE_ROOT'])
with (r/'calls').open('a') as f:f.write(json.dumps(a)+'\\n')
if a[:3]==['image','inspect','aide:dependencies-v2']:
 if (r/'base').exists():print(os.environ.get('BASE_PLATFORM','linux/arm64') if a[-1]=='{{.Os}}/{{.Architecture}}' else 'v2')
 else:sys.exit(1)
elif a[:1]==['info']:print('linux/aarch64')
elif a[:2]==['image','inspect']:
 if os.environ.get('NO_SEED'):sys.exit(1)
 print('sha256:'+'a'*64)
elif a[:2]==['image','load']:(r/'base').touch()
elif a[:2]==['image','tag']:pass
elif a[:1]==['run']:sys.exit(1 if os.environ.get('OLD_CAD') or (os.environ.get('SLIM_SEED') and 'command -v go' in a[-1]) else 0)
elif a[:1]==['build']:
 assert '--pull=false' in a and '--network=none' in a
 (r/'base').touch();sys.exit(int(os.environ.get('BUILD_EXIT','0')))
else:sys.exit('unexpected docker command: '+repr(a))
''');f.chmod(0o755);self.env=dict(os.environ,PATH=str(f.parent)+os.pathsep+os.environ['PATH'],FIXTURE_ROOT=str(self.root))
 def run_prepare(self):return subprocess.run(['bash',str(self.root/'scripts/prepare-local-base.sh')],env=self.env,capture_output=True,text=True)
 def calls(self):return [json.loads(s) for s in (self.root/'calls').read_text().splitlines()]
 def archive(self,valid=True):
  name='aide-dependencies-v2-linux-arm64.tar.gz';payload=b'fixture-image';(self.root/'docker-images'/name).write_bytes(payload)
  (self.root/'docker-images/SHA256SUMS').write_text((hashlib.sha256(payload).hexdigest() if valid else '0'*64)+'  '+name+'\n')
 def test_existing_base_is_reused_without_build_or_load(self):
  (self.root/'base').touch();r=self.run_prepare();self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(r.stdout.strip(),'aide:dependencies-v2');self.assertTrue(all(a[0]=='info' or a[:2]==['image','inspect'] for a in self.calls()))
 def test_wrong_architecture_is_rejected(self):
  (self.root/'base').touch();self.env['BASE_PLATFORM']='linux/amd64';r=self.run_prepare();self.assertNotEqual(r.returncode,0);self.assertFalse(any(a[0]=='build' for a in self.calls()))
 def test_missing_base_is_loaded_from_verified_local_archive(self):
  self.archive();r=self.run_prepare();self.assertEqual(r.returncode,0,r.stderr);self.assertTrue(any(a[:2]==['image','load'] for a in self.calls()));self.assertFalse(any(a[0]=='build' for a in self.calls()))
 def test_corrupt_archive_never_loads_or_builds(self):
  self.archive(False);r=self.run_prepare();self.assertNotEqual(r.returncode,0);self.assertFalse(any(a[0]=='build' or a[:2]==['image','load'] for a in self.calls()))
 def test_local_seed_build_has_no_network_or_pull(self):
  r=self.run_prepare();self.assertEqual(r.returncode,0,r.stderr);self.assertTrue(any(a[0]=='build' and '--network=none' in a and '--pull=false' in a for a in self.calls()))
 def test_no_seed_reports_bootstrap_only(self):
  self.env['NO_SEED']='1';r=self.run_prepare();self.assertEqual(r.returncode,3);self.assertFalse(any(a[0]=='build' for a in self.calls()))
 def test_old_cad_seed_is_rejected_without_build(self):
  self.env['OLD_CAD']='1';r=self.run_prepare();self.assertEqual(r.returncode,3);self.assertIn('CAD-containing',r.stderr);self.assertFalse(any(a[0]=='build' for a in self.calls()))
 def test_slim_seed_reports_development_environment_required(self):
  self.env['SLIM_SEED']='1';r=self.run_prepare();self.assertEqual(r.returncode,3);self.assertIn('development toolbox',r.stderr);self.assertFalse(any(a[0]=='build' for a in self.calls()))
 def test_failed_local_build_is_not_replaced_with_online_build(self):
  self.env['BUILD_EXIT']='9';r=self.run_prepare();self.assertEqual(r.returncode,9)
if __name__=='__main__':unittest.main()
