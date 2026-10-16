"""Native CAD dependencies survive cache cleanup and imported-image recovery."""
import json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class CadDependencyTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='aide cad base ');self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
  (self.root/'scripts').mkdir();(self.root/'bin').mkdir();shutil.copy(ROOT/'scripts/prepare-cad-base.sh',self.root/'scripts')
  docker=self.root/'bin/docker';docker.write_text('''#!/usr/bin/env python3
import os,sys,json,pathlib
r=pathlib.Path(os.environ['FIXTURE_ROOT']);a=sys.argv[1:]
with (r/'calls').open('a') as f:f.write(json.dumps(a)+'\\n')
if a[0]=='info':print('linux/aarch64')
elif a[:2]==['image','inspect']:
 if a[-1]=='{{.Os}}/{{.Architecture}}':print(os.environ.get('IMAGE_PLATFORM','linux/arm64'))
 elif a[2]=='aide:cad-dependencies-v1' and (r/'cad').exists():print('8.0.1.1.0')
 elif a[2]=='aide:local' and (r/'seed').exists():print('8.0.1.1.0')
 else:sys.exit(1)
elif a[:2]==['image','tag']:(r/'cad').touch()
else:sys.exit('Unexpected download/build/mutation: '+repr(a))
''');docker.chmod(0o755);self.env={**os.environ,'PATH':str(docker.parent)+os.pathsep+os.environ['PATH'],'FIXTURE_ROOT':str(self.root)}
 def run_prepare(self):return subprocess.run(['bash','scripts/prepare-cad-base.sh'],cwd=self.root,env=self.env,capture_output=True,text=True)
 def calls(self):return [json.loads(x) for x in (self.root/'calls').read_text().splitlines()]
 def test_retained_base_has_no_build_download_or_tag(self):
  (self.root/'cad').touch();result=self.run_prepare();self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(result.stdout.strip(),'aide:cad-dependencies-v1');self.assertFalse(any(a[:2]==['image','tag'] for a in self.calls()))
 def test_imported_complete_app_recovers_native_dependencies(self):
  (self.root/'seed').touch();result=self.run_prepare();self.assertEqual(result.returncode,0,result.stderr);self.assertTrue(any(a==['image','tag','aide:local','aide:cad-dependencies-v1'] for a in self.calls()))
 def test_missing_is_explicit_not_online_fallback(self):
  result=self.run_prepare();self.assertEqual(result.returncode,3);self.assertIn('Bootstrap once',result.stderr);self.assertFalse(any(a[:2]==['image','tag'] for a in self.calls()))
 def test_wrong_architecture_never_retains_seed(self):
  (self.root/'seed').touch();self.env['IMAGE_PLATFORM']='linux/amd64';result=self.run_prepare();self.assertNotEqual(result.returncode,0);self.assertFalse(any(a[:2]==['image','tag'] for a in self.calls()))
if __name__=='__main__':unittest.main()
