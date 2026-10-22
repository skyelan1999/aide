from pathlib import Path,PurePosixPath
import zipfile,json,subprocess
p=Path('.agent-state/publish-assets/aide-v0.1.17.5-RC3-source.zip');prefix='aide-v0.1.17.5-RC3/'
tracked=set(subprocess.check_output(['git','ls-tree','-r','-z','--name-only','v0.1.17.5-RC3'],text=True).split('\0'))
with zipfile.ZipFile(p) as z:
 assert z.testzip() is None
 files=[]
 for info in z.infolist():
  assert info.filename.startswith(prefix)
  rel=info.filename[len(prefix):]
  if info.is_dir():continue
  assert rel in tracked,rel
  parts=PurePosixPath(rel).parts
  assert not any(x in parts for x in ['.env','.agent-state','.professional-project-state','access-token','issuer-private.pem'])
  assert '.upgrade-backup-' not in rel
  if rel.startswith('plugins/'):
   assert rel.startswith('plugins/testdata/'),rel
   files.append(rel)
 assert len(files)==2,files
Path('.agent-state/source-zip-audit.json').write_text(json.dumps({'status':'pass','sourceZIPPluginScope':'root plugins directory contains only two public testdata files','privatePluginSourceAbsent':True,'credentialsAndWorkspaceAbsent':True,'everyFileBelongsToReleaseTag':True},indent=2)+'\n')
print('PASS: tag-only source ZIP; no private installed plugins, cache or credentials')
