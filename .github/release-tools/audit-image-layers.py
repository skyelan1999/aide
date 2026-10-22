import sys,tarfile,json
from pathlib import Path
arch=sys.argv[1]
allowed={'archive-tools','browser-control','comm-serial','comm-ssh','comm-tcp','comm-udp','computer-control','current-time','environment-guide','feedback','goal','lunar-calendar','markdown-history','office','plan','plugin-authoring','propeller-aero','skill','sqlite','subagent','terminal','todo','workflow'}
private=['compare','starlink','testbench','brdplan','3dmodulebox','pjplan','rftoolbox','professionalproject','professional-project','professional-workflow','almanac'];found=[];layers=0;scanned=0;blobs=[]
p=Path('.agent-state/release-packages')/arch/('aide-v0.1.17.5-RC3-linux-'+arch+'-image.tar.gz')
with tarfile.open(p,'r|gz') as inventory:
 for m in inventory:
  if m.name=='manifest.json':
   expected=set(json.load(inventory.extractfile(m))[0]['Layers']);break
 else:raise RuntimeError('Missing Docker manifest')
with tarfile.open(p,'r|gz') as outer:
 for blob in outer:
  if not blob.isfile() or blob.name not in expected:continue
  stream=outer.extractfile(blob)
  try:
   inner=tarfile.open(fileobj=stream,mode='r|*')
  except tarfile.ReadError as e:raise RuntimeError('Cannot audit referenced image layer '+blob.name) from e
  layers+=1;blobs.append(blob.name)
  with inner:
   for entry in inner:
    scanned+=1;name=entry.name.lower()
    assert not any('/plugins/'+x+'/' in '/'+name or 'aide-pkg-'+x in name for x in private),entry.name
    assert '.upgrade-backup-' not in name and '.professional-project-state' not in name,entry.name
    if entry.isfile() and name.endswith('/manifest.json'):
     assert entry.size<4*1024*1024
     try:d=json.loads(inner.extractfile(entry).read())
     except (ValueError,UnicodeError):continue
     if isinstance(d,dict) and 'id' in d and 'main' in d:
      assert d['id'] in allowed,(entry.name,d['id']);found.append({'path':entry.name,'id':d['id']})
assert set(blobs)==expected,(set(blobs),expected)
result={'status':'pass','platform':'linux/'+arch,'layerTarArchivesScanned':layers,'allReferencedLayersScanned':True,'layerEntriesScanned':scanned,'allLayersPluginManifests':found,'noPrivatePluginDirectoriesOrUpgradeBackups':True}
Path('.agent-state/private-plugin-layer-audit-'+arch+'.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
