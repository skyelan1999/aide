from pathlib import Path,PurePosixPath
import json,zipfile,hashlib,subprocess,sys
arch=sys.argv[1];tag='v0.1.17.5-RC3';p=Path('.agent-state/release-packages')/arch/('aide-'+tag+'-full-linux-'+arch+'.zip');root='aide-'+tag+'-full-linux-'+arch
identity=json.loads(subprocess.check_output(['docker','image','inspect','aide:0.1.17.5-RC3-linux-'+arch],text=True))[0]
with zipfile.ZipFile(p) as z:
 names=z.namelist();assert len(names)==len(set(names))
 for name in names:
  parts=PurePosixPath(name).parts
  assert parts and parts[0]==root and '..' not in parts and not name.startswith('/')
  assert not any(x in parts for x in ['.env','.aide-data','access-token','issuer-private.pem'])
  if 'workspace' in parts or 'context' in parts:assert name.endswith('/')
 m=json.loads(z.read(root+'/docker-images/manifest.json'));assert m['releaseTag']==tag and m['platform']=='linux/'+arch and m['imageId']==identity['Id']
 sums=z.read(root+'/docker-images/SHA256SUMS').decode().split();assert len(sums)==2 and sums[1]==m['imageArchive']
 h=hashlib.sha256()
 with z.open(root+'/docker-images/'+m['imageArchive']) as f:
  for b in iter(lambda:f.read(1<<20),b''):h.update(b)
 assert h.hexdigest()==sums[0]
 assert z.read(root+'/.aide-image').decode().split()[1:]==[identity['Id'],'linux/'+arch,tag]
 for n in names:
  if n.endswith('/') or n.endswith('/'+m['imageArchive']):continue
  z.read(n)
print(json.dumps({'status':'pass','platform':'linux/'+arch,'file':p.name,'imageId':identity['Id'],'archiveSHA256':h.hexdigest(),'safeMemberNames':True,'noUserData':True,'payloadCRCAndSHA256':True}))
