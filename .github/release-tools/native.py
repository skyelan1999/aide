"""Execute the existing immutable-tag build/package tools on native hosted CPUs."""
import hashlib,importlib.util,json,os,platform,shutil,subprocess,sys,zipfile
from pathlib import Path
TAG='v0.1.17.5-RC3';COMMIT='e1691cc401bd495d5013552c83dcfef77c584450';SOURCE='caa52838c0d7bb4cb54b1bf8b505c3d5ebe5a90b682b0e5da943417504dc9cac'
TOOLS=Path(__file__).resolve().parent;STATE=Path('.agent-state');STATE.mkdir(exist_ok=True)
def run(args,**kw):
 if 'stdout' not in kw:return subprocess.run(args,check=True,**kw)
 log=kw.pop('stdout');kw.pop('stderr',None)
 proc=subprocess.Popen(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,**kw)
 for line in proc.stdout:
  log.write(line);log.flush();print(line,end='',flush=True)
 code=proc.wait()
 if code:raise subprocess.CalledProcessError(code,args)
 return proc
def capture(args):return subprocess.check_output(args,text=True).strip()
def digest(p):
 h=hashlib.sha256()
 with Path(p).open('rb') as f:
  for b in iter(lambda:f.read(8<<20),b''):h.update(b)
 return h.hexdigest()
def main():
 assert os.environ.get('GITHUB_ACTIONS')=='true'
 arch=sys.argv[1];assert platform.machine()=={'arm64':'aarch64','amd64':'x86_64'}[arch]
 assert capture(['git','rev-parse','HEAD'])==COMMIT
 assert capture(['bash','scripts/aide.sh','source-sha'])==SOURCE
 spec=importlib.util.spec_from_file_location('gate','scripts/agent-route.py');gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)
 # Full local gate is checked by the final publish job after parallel native builds.
 base='aide:0.1.17.4-RC2-linux-'+arch;name='aide-v0.1.17.4-RC2-full-linux-'+arch+'.zip'
 prior=json.loads(capture(['gh','release','view','v0.1.17.4-RC2','--json','assets']))
 meta=next(a for a in prior['assets'] if a['name']==name)
 run(['gh','release','download','v0.1.17.4-RC2','--pattern',name,'--dir',str(STATE)])
 bundle=STATE/name;assert meta['digest']=='sha256:'+digest(bundle)
 with zipfile.ZipFile(bundle) as z:
  prefix=name[:-4]+'/docker-images/';manifest=json.loads(z.read(prefix+'manifest.json'))
  assert manifest['platform']=='linux/'+arch and manifest['releaseTag']=='v0.1.17.4-RC2'
  sums=z.read(prefix+'SHA256SUMS').decode().split();assert len(sums)==2 and sums[1]==manifest['imageArchive']
  archive=STATE/'base-image.tar.gz'
  with z.open(prefix+manifest['imageArchive']) as src,archive.open('wb') as out:shutil.copyfileobj(src,out,8<<20)
  assert digest(archive)==sums[0]
 run(['docker','load','--input',str(archive)]);bundle.unlink();archive.unlink()
 base_meta=json.loads(capture(['docker','image','inspect',base]))[0];assert base_meta['Architecture']==arch
 # Use the repository's existing application-free dependency conversion.
 # This matches the locally verified release build instead of leaving a prior
 # installed aide executable in the build-stage PATH.
 seed=base;base='aide:release-dependencies-'+arch
 run(['docker','build','--network','none','--progress','plain','-f','docker/Dockerfile.local-base','--build-arg','AIDE_SEED_IMAGE='+seed,'-t',base,'.'])
 image='aide:0.1.17.5-RC3-linux-'+arch
 with (STATE/('build-'+arch+'.log')).open('w') as log:
  run(['docker','build','--platform','linux/'+arch,'--network','none','--progress','plain','-f','Dockerfile.local','--build-arg','AIDE_DEPENDENCY_IMAGE='+base,'--build-arg','AIDE_CAD_IMAGE='+base,'--build-arg','AIDE_VERSION=0.1.17.5 RC3','--build-arg','AIDE_COMMIT='+COMMIT,'--build-arg','AIDE_RUNTIME_MODE=release-image','--build-arg','AIDE_SRC_SHA='+SOURCE,'--build-arg','AIDE_RUN_TESTS=1','-t',image,'.'],stdout=log,stderr=subprocess.STDOUT)
 for target,args in [('runtime',['python3','scripts/test_runtime_assets.py',image,'--require-lan-bridge']),('smoke',['python3',str(TOOLS/'smoke.py'),image,arch,COMMIT])]:
  with (STATE/(target+'-'+arch+'.log')).open('w') as log:run(args,stdout=log,stderr=subprocess.STDOUT)
 out=STATE/'release-packages'/arch
 run(['bash','scripts/package-release-assets.sh',TAG,image,str(out)])
 run(['python3',str(TOOLS/'audit-image-layers.py'),arch]);run(['python3',str(TOOLS/'validate-full-package.py'),arch])
 run(['docker','load','--input',str(out/('aide-'+TAG+'-linux-'+arch+'-image.tar.gz'))])
 info=json.loads(capture(['docker','image','inspect',image]))[0];assert info['Config']['Labels']['aide.srcsha']==SOURCE
 with (STATE/('smoke-'+arch+'.log')).open('w') as log:run(['python3',str(TOOLS/'smoke.py'),image,arch,COMMIT],stdout=log,stderr=subprocess.STDOUT)
 artifact=out/('aide-'+TAG+'-full-linux-'+arch+'.zip')
 proof={'status':'pass','platform':'linux/'+arch,'runner':os.environ['RUNNER_NAME'],'commit':COMMIT,'sourceSHA256':SOURCE,'imageId':info['Id'],'baseImageId':base_meta['Id'],'fullGoTests':'native build pass','smoke':json.loads((STATE/('smoke-'+arch+'.log')).read_text().splitlines()[-1]),'privacy':json.loads((STATE/('private-plugin-layer-audit-'+arch+'.json')).read_text()),'archiveImported':True,'asset':{'name':artifact.name,'bytes':artifact.stat().st_size,'sha256':digest(artifact)},'runURL':'https://github.com/'+os.environ['GITHUB_REPOSITORY']+'/actions/runs/'+os.environ['GITHUB_RUN_ID']}
 (STATE/('native-'+arch+'.json')).write_text(json.dumps(proof,indent=2)+'\n')
 assert json.loads(capture(['gh','release','view',TAG,'--json','isDraft']))['isDraft']
 run(['gh','release','upload',TAG,str(artifact)])
 print(json.dumps(proof),flush=True)
if __name__=='__main__':main()
