from pathlib import Path
import zipfile
root=Path(__file__).resolve().parent
html=(root/'viewer.src.html').read_text().replace('/* BRIDGE */',(root.parent.parent/'internal/server/web/plugin-workbench-client.js').read_text())
(root/'viewer.html').write_text(html)
with zipfile.ZipFile(root/'example-plugin.zip','w',zipfile.ZIP_DEFLATED) as z:
 files={name:(root/name).read_bytes() for name in ['manifest.json','index.js','viewer.html','README.md']}
 files['LICENSE']=(root.parent.parent/'LICENSE').read_bytes()
 for name in sorted(files):
  info=zipfile.ZipInfo(name,(2026,10,21,0,0,0));info.compress_type=zipfile.ZIP_DEFLATED;info.external_attr=0o100644<<16;z.writestr(info,files[name])
