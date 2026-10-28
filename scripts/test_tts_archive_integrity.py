"""Exercise the actual shell installer with synthetic archives; no model execution/network."""
import hashlib, io, os, pathlib, re, subprocess, tarfile, tempfile, unittest
ROOT=pathlib.Path(__file__).resolve().parents[1]
SOURCE=(ROOT/'scripts/tts-setup').read_text()
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def archive(p,files):
 with tarfile.open(p,'w:bz2') as t:
  for name,value in files.items():
   data=value.encode();m=tarfile.TarInfo(name);m.size=len(data);t.addfile(m,io.BytesIO(data))
class Integrity(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='aide-tts-review-');self.root=pathlib.Path(self.tmp.name);self.good=self.root/'renamed.tar.bz2';archive(self.good,{'voice/model.onnx':'synthetic-model','voice/tokens.txt':'a 0'})
 def tearDown(self):self.tmp.cleanup()
 def run_cli(self,*args,script=None,env=None):return subprocess.run(['bash',str(script or ROOT/'scripts/tts-setup'),*map(str,args)],text=True,capture_output=True,env=env,timeout=15)
 def test_verify_explicit_trusted_digest(self):
  h=digest(self.good);self.assertEqual(self.run_cli('--verify-only',self.good,'--sha256',h.upper()).returncode,0)
  self.good.write_bytes(self.good.read_bytes()+b'changed');self.assertNotEqual(self.run_cli('--verify-only',self.good,'--sha256',h).returncode,0)
 def test_missing_or_invalid_digest_fails(self):
  self.assertNotEqual(self.run_cli('--verify-only',self.good).returncode,0)
  self.assertNotEqual(self.run_cli('--verify-only',self.good,'--sha256','no').returncode,0)
 def test_known_name_cannot_bypass_pin(self):
  p=self.root/'vits-piper-zh_CN-huayan-medium.tar.bz2';p.write_bytes(self.good.read_bytes());self.assertNotEqual(self.run_cli('--verify-only',p).returncode,0)
 def installer(self,files,corrupt_model=False,corrupt_espeak=False,missing_phonemes=False):
  model=self.root/'model.tar.bz2';archive(model,files);espeak=self.root/'espeak.tar.bz2';archive(espeak,{'espeak-ng-data/README':'missing phonemes'} if missing_phonemes else {'espeak-ng-data/phontab':'phonemes'})
  source=SOURCE.replace('dbdfec42b91d9cee31cce9ff4b3e9c305eb6fbf60546d071f7e46273554cce6b',digest(model)).replace('4135ccf82e1f40613491c0874d4945ae9e9c7840933d8e25a6f9e003d9ebf533',digest(espeak));script=self.root/'tts-setup';script.write_text(source)
  if corrupt_model:model.write_bytes(model.read_bytes()+b'changed')
  if corrupt_espeak:espeak.write_bytes(espeak.read_bytes()+b'changed')
  bindir=self.root/'bin';bindir.mkdir();curl=bindir/'curl';curl.write_text('#!/usr/bin/env python3\nimport pathlib,shutil,sys\na=sys.argv[1:];out=a[a.index("-o")+1];name="espeak.tar.bz2" if a[-1].endswith("espeak-ng-data.tar.bz2") else "model.tar.bz2"\nshutil.copyfile(pathlib.Path(__file__).resolve().parents[1]/name,out)\n');curl.chmod(0o755)
  env=dict(os.environ,PATH=str(bindir)+os.pathsep+os.environ['PATH'],TMPDIR=str(self.root));target=self.root/'installed';r=self.run_cli('--model','huayan','--dir',target,'--mirror','https://owned.invalid',script=script,env=env);return r,target
 def test_valid_install_has_required_files(self):
  r,target=self.installer({'voice/model.onnx':'synthetic-model','voice/tokens.txt':'a 0'});self.assertEqual(r.returncode,0,r.stderr+r.stdout);self.assertTrue((target/'huayan/model.onnx').is_file());self.assertTrue((target/'huayan/espeak-ng-data/phontab').is_file());self.assertTrue((target/'huayan/meta.json').is_file())
 def test_modified_model_does_not_install(self):
  r,target=self.installer({'voice/model.onnx':'synthetic-model','voice/tokens.txt':'a 0'},corrupt_model=True);self.assertNotEqual(r.returncode,0);self.assertFalse(target.exists())
 def test_modified_espeak_does_not_install(self):
  r,target=self.installer({'voice/model.onnx':'synthetic-model','voice/tokens.txt':'a 0'},corrupt_espeak=True);self.assertNotEqual(r.returncode,0);self.assertFalse(target.exists())
 def test_missing_phonemes_does_not_install(self):
  r,target=self.installer({'voice/model.onnx':'synthetic-model','voice/tokens.txt':'a 0'},missing_phonemes=True);self.assertNotEqual(r.returncode,0);self.assertFalse(target.exists())
 def test_missing_tokens_does_not_install(self):
  r,target=self.installer({'voice/model.onnx':'synthetic-model'});self.assertNotEqual(r.returncode,0);self.assertFalse(target.exists())
 def test_missing_onnx_does_not_install(self):
  r,target=self.installer({'voice/tokens.txt':'a 0'});self.assertNotEqual(r.returncode,0);self.assertFalse(target.exists())
if __name__=='__main__':unittest.main()
