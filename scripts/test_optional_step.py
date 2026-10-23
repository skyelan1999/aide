#!/usr/bin/env python3
"""Default distribution excludes optional STEP payloads; keep DXF/STL assets."""
from pathlib import Path
import subprocess,sys,os
ROOT=Path(__file__).resolve().parents[1]
def check_source():
 for name in ('Dockerfile','Dockerfile.local','compose.yaml','docker/compose.offline.yaml','scripts/aide.sh','start.ps1'):
  text=(ROOT/name).read_text()
  for banned in ('AIDE_CAD_IMAGE','AIDE_CAD_URL','prepare-cad-base','scripts/cad','cad-private','--from=cad'):
   assert banned not in text,(name,banned)
 web=ROOT/'internal/server/web'
 for name in ('step-preview.js','step-import-worker.js','step-metrology.js','step-assembly.js','vendor/occt-import-js'):assert not (web/name).exists(),name
 for name in ('vendor/dxf-parser/dxf-parser.js','vendor/three.min.js','vendor/OrbitControls.js','urdf-preview.js'):assert (web/name).is_file(),name
 assert 'dxf-parser + SVG' in (web/'app.js').read_text()
 assert 'test ! -e /opt/aide-cad' in (ROOT/'docker/Dockerfile.local-base').read_text()
check_source()
image=sys.argv[1] if len(sys.argv)>1 else os.environ.get('AIDE_TEST_IMAGE','aide:local')
subprocess.run(['docker','run','--rm','--network=none','--entrypoint','sh',image,'-ec',
 'test ! -e /opt/aide-cad; test ! -e /opt/aide/cad; test ! -e /data/plugins/step-viewer'],check=True,timeout=30)
print('PASS: default image has no STEP engine/plugin; DXF/STL assets retained')
