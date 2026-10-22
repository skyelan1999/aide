"""Publish only after native jobs and GitHub's independent asset digests agree."""
import base64,hashlib,importlib.util,json,os,subprocess,time
from pathlib import Path
TAG='v0.1.17.5-RC3';COMMIT='e1691cc401bd495d5013552c83dcfef77c584450';SOURCE='caa52838c0d7bb4cb54b1bf8b505c3d5ebe5a90b682b0e5da943417504dc9cac'
STATE=Path('.agent-state');TOOLS=Path(__file__).resolve().parent
out=STATE/'publish-assets';out.mkdir(parents=True,exist_ok=True)
def capture(args):return subprocess.check_output(args,text=True).strip()
def run(args):subprocess.run(args,check=True)
def digest(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for b in iter(lambda:f.read(8<<20),b''):h.update(b)
 return h.hexdigest()
def release():return json.loads(capture(['gh','release','view',TAG,'--json','tagName,isDraft,isPrerelease,url,assets']))
def check_assets(expected):
 d=release();assets={a['name']:a for a in d['assets']}
 for name,item in expected.items():
  a=assets[name];assert a['state']=='uploaded' and a['size']==item['bytes'] and a['digest']=='sha256:'+item['sha256'],name
 return d
assert os.environ.get('GITHUB_ACTIONS')=='true' and capture(['git','rev-parse','HEAD'])==COMMIT
spec=importlib.util.spec_from_file_location('gate','scripts/agent-route.py');gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)
expectedFP=gate.fingerprint()
assert release()['isDraft']
check_assets(json.loads((TOOLS/'draft-metadata.json').read_text()))
local=json.loads((TOOLS/'local-full-receipt.json').read_text())
assert local.get('status')=='pass' and local.get('fingerprint')==expectedFP
fullGate={'checks':len(local['results']),'fingerprint':local['fingerprint'],'status':local['status']}
proofs=[json.loads((STATE/'receipts'/('native-'+a+'.json')).read_text()) for a in ['arm64','amd64']]
for p in proofs:p['localFullGate']=fullGate
for p in proofs:
 assert p['status']=='pass' and p['commit']==COMMIT and p['sourceSHA256']==SOURCE and p['archiveImported'] and p['privacy']['allReferencedLayersScanned']
 assert p['fullGoTests']=='native build pass' and p['smoke']['status']=='pass'
assert {p['platform'] for p in proofs}=={'linux/arm64','linux/amd64'}
expected={p['asset']['name']:p['asset'] for p in proofs};check_assets(expected)
source=out/('aide-'+TAG+'-source.zip');run(['git','archive','--format=zip','--prefix=aide-'+TAG+'/','-o',str(source),TAG]);run(['python3',str(TOOLS/'source-audit.py')])
expected[source.name]={'bytes':source.stat().st_size,'sha256':digest(source)}
assert release()['isDraft']
run(['gh','release','upload',TAG,str(source)])
hotfix=out/'aide-v0.1.17.5-RC2-windows-launcher-fix.zip'
run(['gh','release','download','v0.1.17.5-RC2','--pattern',hotfix.name,'--dir',str(out)])
assert digest(hotfix)=='e82e79413011a2a03cfe09f3e13c8e84f7dc2b365ede80b22a18e6c9d4d397e5'
import zipfile
with zipfile.ZipFile(hotfix) as z:
 assert z.testzip() is None and z.read('start.ps1')==Path('start.ps1').read_bytes()
expected[hotfix.name]={'bytes':hotfix.stat().st_size,'sha256':digest(hotfix)}
check_assets(expected)
note={'releaseTag':TAG,'commit':COMMIT,'sourceSHA256':SOURCE,'fullGate':proofs[0]['localFullGate'],'platforms':proofs,'privacy':{'source':json.loads((STATE/'source-zip-audit.json').read_text()),'privatePluginPackagesPublished':False,'userDataIncluded':False},'distribution':'One full ZIP for each Docker architecture and immutable-tag source ZIP. Complete runtime and A/B upgrades use existing package-release-assets.sh. Published only after all six independent remote digests match.','validationBoundary':{'linuxARM64':'Native hosted build full Go tests, runtime assets, authenticated smoke, embedded asset hashes, all image layers, ZIP CRC/SHA and Docker import passed','linuxAMD64':'Native hosted build full Go tests, runtime assets, authenticated smoke, embedded asset hashes, all image layers, ZIP CRC/SHA and Docker import passed','localFullGate':'145 checks including 1109 server cases in 12 race shards plus vet passed','browser':'No UI changes; both hosted images verify 94 embedded assets by SHA256 and C# grammar MIME through HTTPS; user reports Windows recovered after rerunning, initial one-time asset failure cause unconfirmed','nativeWindowsAndMacLaunch':'Windows PowerShell 5.1 and 7 isolated bootstrap fixtures passed; Windows Docker Desktop end-to-end not instrumented; user reported rerun recovered; macOS host launch not_run','currentUserRuntimeReplaced':False},'runURL':'https://github.com/'+os.environ['GITHUB_REPOSITORY']+'/actions/runs/'+os.environ['GITHUB_RUN_ID']}
notes=out/'RELEASE-ASSETS.txt';notes.write_text(json.dumps(note,ensure_ascii=False,indent=2)+'\n');expected[notes.name]={'bytes':notes.stat().st_size,'sha256':digest(notes)}
sums=out/'SHA256SUMS';sums.write_text(''.join(expected[n]['sha256']+'  '+n+'\n' for n in sorted(expected)));expected[sums.name]={'bytes':sums.stat().st_size,'sha256':digest(sums)}
assert release()['isDraft']
run(['gh','release','upload',TAG,str(notes),str(sums),'--clobber'])
d=check_assets(expected);assert set(a['name'] for a in d['assets'])==set(expected)
ref=json.loads(capture(['gh','api','repos/skyelan1999/aide/git/ref/tags/'+TAG]));obj=json.loads(capture(['gh','api','repos/skyelan1999/aide/git/tags/'+ref['object']['sha']]));assert obj['object']['sha']==COMMIT
body=Path('docs/reviews/release-0.1.17.5-RC3-2026-10-22.md').read_text()
body=body.replace('Windows 修复已在 PowerShell 7 的隔离 Docker 测试中复现和验证；Windows 真机和 Windows PowerShell 5.1 仍待用户现场验收。','Windows 修复已通过 Windows PowerShell 5.1 和 PowerShell 7 的隔离启动回归；Windows Docker Desktop 完整链路尚未做仪器化验收，已有用户反馈重新运行后恢复正常。')
bodyfile=STATE/'release-notes.md';bodyfile.write_text(body)
run(['gh','release','edit',TAG,'--draft=false','--notes-file',str(bodyfile)])
d=check_assets(expected);assert not d['isDraft'] and d['isPrerelease']
receipt={'status':'pass','tag':TAG,'commit':COMMIT,'url':d['url'],'remoteSHA256Matches':True,'assets':expected,'runURL':note['runURL'],'privatePluginPackagesPublished':False,'currentUserRuntimeReplaced':False}
(STATE/'native-published.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt),flush=True)
