#!/usr/bin/env python3
"""Actual native helper + Docker NFS + isolated Aide API acceptance.

Requires already-built native helper/Linux Aide binary and a prepared local
runtime image. Never invokes the user's Aide project or publishes a port.
"""
import argparse,hashlib,json,os,shutil,subprocess,sys,time
from pathlib import Path


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--helper',type=Path,required=True)
    p.add_argument('--app-binary',type=Path,required=True)
    p.add_argument('--image',required=True)
    p.add_argument('--work',type=Path,required=True)
    args=p.parse_args()
    helper=args.helper.resolve();app=args.app_binary.resolve();work=args.work.resolve()
    if work.exists():raise ValueError('Use a new disposable work directory')
    work.mkdir(parents=True,mode=0o700);root=work/'project';root.mkdir();inputs=work/'inputs';inputs.mkdir()
    rw=inputs/'rw';ro=inputs/'ro-parent'/'selected';outside=inputs/'outside';outside_ro=inputs/'outside-parent'/'selected'
    for d in [rw,ro,outside,outside_ro]:d.mkdir(parents=True,mode=0o700)
    for d,text in [(rw,'ORIGINAL-RW'),(ro,'ORIGINAL-RO'),(outside,'OUTSIDE-RW'),(outside_ro,'OUTSIDE-RO')]:
        (d/'marker.txt').write_text(text)
    project='aide-object-bridge-'+str(os.getpid())
    base={'name':project,'services':{'aide':{'image':args.image,'user':'1000:1000','entrypoint':[],
        'command':['/probe/aide-candidate'],'network_mode':'none','mem_limit':'2500m','cpus':2,
        'volumes':[{'type':'bind','source':str(app),'target':'/probe/aide-candidate','read_only':True}],
        'environment':{'AI_API_KEY':'','AIDE_HOST_LOCAL':'/local'}}}}
    (root/'compose.yaml').write_text(json.dumps(base,indent=2))
    desired=root/'.aide-data/storage/data/local-mounts/desired.json';desired.parent.mkdir(parents=True)
    desired.write_text(json.dumps({'version':1,'entries':[{'id':'rw','name':'RW fixture','path':str(rw),'readOnly':False},{'id':'ro','name':'RO fixture','path':str(ro),'readOnly':True}]}))
    env={k:v for k,v in os.environ.items() if k not in ['COMPOSE_FILE','COMPOSE_PROJECT_NAME','AIDE_LOCAL_MOUNTS_COMPOSE']}
    log=(work/'private.log').open('ab');override=root/'.aide-data/local-mounts/compose.json'
    compose=['docker','compose','-p',project,'-f',str(root/'compose.yaml'),'-f',str(override)]
    receipts=[];names=[];container=''
    def run(cmd,**kw):return subprocess.run(cmd,env=env,cwd=root,check=True,stdout=log,stderr=log,**kw)
    def capture(cmd,**kw):return subprocess.check_output(cmd,env=env,cwd=root,stderr=log,**kw)
    def inside(script,*extra):return capture(['docker','exec',container,'python3','-c',script,*extra],timeout=45)
    try:
        run([str(helper),'-mode','prepare','-root',str(root),'-docker','docker'],timeout=125)
        cfg=json.loads(override.read_text());names=list(cfg['volumes']);assert len(names)==2
        mounts=cfg['services']['aide']['volumes'];assert all(m['type']=='volume' for m in mounts[:-1])
        assert not any(m.get('source') in [str(rw),str(ro)] for m in mounts)
        # Replace the selected directory and, separately, a selected parent
        # after native preparation but before Docker opens the volume.
        rw.rename(inputs/'held-rw');rw.symlink_to(outside,target_is_directory=True)
        (inputs/'ro-parent').rename(inputs/'held-ro-parent');(inputs/'ro-parent').symlink_to(inputs/'outside-parent',target_is_directory=True)
        run(compose+['up','-d','--no-build','--pull','never'],timeout=90)
        container=capture(compose+['ps','-q','aide'],text=True).strip();assert container
        assert inside("from pathlib import Path;print(Path('/mounts/rw/marker.txt').read_text());print(Path('/mounts/ro/marker.txt').read_text())").decode().splitlines()==['ORIGINAL-RW','ORIGINAL-RO']
        api=r'''
import json,ssl,time,urllib.request,sys
from pathlib import Path
for i in range(120):
 try:
  token=next(p for p in [Path('/data/auth/access-token'),Path('/data/access-token')] if p.is_file()).read_text().strip()
  c=ssl._create_unverified_context()
  def request(url,body=None):
   raw=None if body is None else json.dumps(body).encode();r=urllib.request.Request('https://127.0.0.1:8080'+url,data=raw,method='GET' if body is None else 'PUT',headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'});return urllib.request.urlopen(r,context=c,timeout=10).read()
  request('/healthz');break
 except Exception:time.sleep(.2)
else:raise RuntimeError('isolated Aide health timeout')
request('/api/workspace-config',{'workspace':{'mode':'local','path':sys.argv[1]}})
raw=request('/api/file/raw?root=workspace&path=marker.txt')
assert raw==b'ORIGINAL-RW',raw
print('ACTUAL-AIDE-API-ORIGINAL-OBJECT')
'''
        assert inside(api,str(rw)).strip()==b'ACTUAL-AIDE-API-ORIGINAL-OBJECT'
        # Exercise multi-request stable writes, rename and create from uid 1000.
        output=inside("from pathlib import Path;import os,hashlib;p=Path('/mounts/rw/new-dir');p.mkdir();b=os.urandom(3<<20);f=(p/'new.bin').open('wb');f.write(b);f.flush();os.fsync(f.fileno());f.close();(p/'new.bin').rename(p/'saved.bin');print(hashlib.sha256(b).hexdigest())").decode().strip()
        saved=inputs/'held-rw/new-dir/saved.bin';assert hashlib.sha256(saved.read_bytes()).hexdigest()==output
        assert sorted(p.name for p in outside.iterdir())==['marker.txt']
        assert sorted(p.name for p in outside_ro.iterdir())==['marker.txt']
        ro_name=next(m['source'] for m in mounts if m['target']=='/mounts/ro')
        # Request a RW client mount of the RO export: server permission still wins.
        result=subprocess.run(['docker','run','--rm','--network','none','--user','1000:1000','--mount','type=volume,src='+ro_name+',target=/readonly','--entrypoint','sh',args.image,'-c','printf DENIED > /readonly/denied.txt'],env=env,stdout=log,stderr=log,timeout=30)
        assert result.returncode!=0 and not (inputs/'held-ro-parent/selected/denied.txt').exists()
        # A new process must reject changed persisted object identities rather
        # than silently bind the replacement after losing the original handles.
        before={f:f.read_bytes() for f in [override,root/'.aide-data/local-mounts/active.json',root/'.aide-data/local-mounts/bridge-identities.json']}
        result=subprocess.run([str(helper),'-mode','prepare','-root',str(root),'-docker','docker'],env=env,cwd=root,stdout=log,stderr=log,timeout=125)
        assert result.returncode!=0
        assert all(f.read_bytes()==b for f,b in before.items())
        assert inside("from pathlib import Path;print(Path('/mounts/rw/marker.txt').read_text())").strip()==b'ORIGINAL-RW'
        receipts={'status':'PASS','helperSHA256':hashlib.sha256(helper.read_bytes()).hexdigest(),'appSHA256':hashlib.sha256(app.read_bytes()).hexdigest(),'image':capture(['docker','image','inspect',args.image,'--format','{{.Id}}'],text=True).strip(),'directorySwap':'original object retained','parentSwap':'original object retained','actualAideAPI':'workspace path maps to retained object','uid':1000,'stableWriteBytes':3<<20,'writeSHA256':output,'ROServerEnforcement':'RW client refused','persistedIdentityChange':'rejected without changing active metadata','outsideDirectories':'unchanged','production':'untouched'}
        (work/'receipt.json').write_text(json.dumps(receipts,indent=2)+'\n');print(json.dumps(receipts))
    finally:
        if override.exists():subprocess.run(compose+['down','--timeout','10'],env=env,cwd=root,stdout=log,stderr=log,timeout=60)
        for f in (root/'.aide-data/local-mounts/bridge-runs').glob('*/ready.json'):
            (f.parent/'stop').write_text('stop')
        for name in names:subprocess.run(['docker','volume','rm',name],env=env,stdout=log,stderr=log,timeout=20)
        log.close()

if __name__=='__main__':
    try:main()
    except Exception as e:
        print('FAIL: '+type(e).__name__+': '+str(e)[:180],file=sys.stderr);raise SystemExit(1)
