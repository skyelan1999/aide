#!/usr/bin/env python3
"""Real converter metadata coverage, forced chunking and inherited fidelity suite."""
import importlib.util
from pathlib import Path
import contextlib
import io
import json
root=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('converter',root/'scripts/cad/convert.py');converter=importlib.util.module_from_spec(spec);spec.loader.exec_module(converter)
def run(batch):
 converter.BATCH_TRIANGLES=batch
 output=io.StringIO()
 with contextlib.redirect_stdout(output):converter.convert(str(root/'scripts/fixtures/cad/fidelity.step'))
 messages=[json.loads(line) for line in output.getvalue().splitlines()];parts={};counts={}
 for item in messages:
  if item['kind'] not in ['native-mesh','native-lines','native-points']:continue
  import base64
  vertices=len(base64.b64decode(item['positions']))//12
  next_start=0
  for part in item['parts']:
   assert part['start']==next_start and part['count']>0
   next_start+=part['count'];parts.setdefault(part['id'],part['name']);assert parts[part['id']]==part['name'];counts[part['id']]=counts.get(part['id'],0)+part['count']
  assert next_start==vertices
 assert len(parts)==messages[-1]['parts']==2
 return parts,counts,len([m for m in messages if m['kind']=='native-mesh'])
a=run(16000);b=run(5);assert a[:2]==b[:2];assert b[2]>a[2],(a,b)
# Same original fidelity checks on the installed pinned host runtime, no Docker claim.
spec=importlib.util.spec_from_file_location('fidelity',root/'scripts/test_cad_geometry.py');fidelity=importlib.util.module_from_spec(spec);spec.loader.exec_module(fidelity)
program=fidelity.PROGRAM.replace('/scripts/cad/convert.py',str(root/'scripts/cad/convert.py')).replace('/scripts/fixtures/cad/fidelity.step',str(root/'scripts/fixtures/cad/fidelity.step')).replace('/fixtures/occt-import-js-0.0.23-source.tgz',str(root/'internal/server/web/vendor/occt-import-js/occt-import-js-0.0.23-source.tgz'))
exec(program)
print(json.dumps({'status':'PASS','parts':a[0],'forcedChunkBatches':b[2],'metadataComplete':True,'stableIdentityAcrossChunks':True}))
