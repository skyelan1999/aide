#!/usr/bin/env python3
"""Real native CAD fidelity checks; opt-in Docker test, no network/user data."""
from pathlib import Path
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[1]
PROGRAM=r'''
import array,base64,json,subprocess,tarfile,tempfile,math
from pathlib import Path
converter='/scripts/cad/convert.py'
def run(path):
 result=subprocess.run(['python3',converter,str(path)],capture_output=True,text=True,check=True,timeout=90)
 lower=[math.inf]*3;upper=[-math.inf]*3;colors=set();triangles=0;complete=None
 for line in result.stdout.splitlines():
  item=json.loads(line)
  assert not item.get('error'),item
  if item.get('kind')=='native-mesh':
   positions=array.array('f');positions.frombytes(base64.b64decode(item['positions']))
   normals=array.array('f');normals.frombytes(base64.b64decode(item['normals']))
   colour=array.array('f');colour.frombytes(base64.b64decode(item['colors']))
   assert len(positions)==len(normals)==len(colour) and len(positions)%9==0
   assert len(positions)//9<=16000
   triangles+=len(positions)//9
   for i,v in enumerate(positions):
    axis=i%3;world=v+item['origin'][axis];lower[axis]=min(lower[axis],world);upper[axis]=max(upper[axis],world)
   colors.update(tuple(round(v,4) for v in colour[i:i+3]) for i in range(0,len(colour),3))
   for i in range(0,len(normals),3):assert abs(math.hypot(*normals[i:i+3])-1)<.0001
   for i in range(0,len(positions),9):
    a=positions[i:i+3];b=positions[i+3:i+6];c=positions[i+6:i+9]
    u=[b[k]-a[k] for k in range(3)];v=[c[k]-a[k] for k in range(3)]
    cross=[u[1]*v[2]-u[2]*v[1],u[2]*v[0]-u[0]*v[2],u[0]*v[1]-u[1]*v[0]]
    length=math.hypot(*cross)
    if length>1e-12:assert sum(cross[k]*normals[i+k] for k in range(3))/length>.7,'triangle orientation disagrees with normal'
  elif item.get('kind')=='complete':complete=item
 assert complete and triangles==complete['triangles']>0 and complete['unit']=='mm'
 for observed,reported in zip(lower+upper,sum(complete['bounds'],[])):assert abs(observed-reported)<.0001
 return complete,colors
# A free XCAF root may carry a non-identity location itself. Node.Location
# replaces the prototype root location; it must not be applied twice.
import importlib.util,contextlib,io
from OCP.BRepPrimAPI import BRepPrimAPI_MakeBox
from OCP.gp import gp_Trsf,gp_Vec
from OCP.TopLoc import TopLoc_Location
from OCP.XCAFDoc import XCAFDoc_DocumentTool
import OCP.STEPCAFControl
original_reader=OCP.STEPCAFControl.STEPCAFControl_Reader
class LocatedRootReader:
 def SetColorMode(self,*a):pass
 def SetNameMode(self,*a):pass
 def SetLayerMode(self,*a):pass
 def ReadFile(self,*a):return 1
 def Transfer(self,doc):
  tool=XCAFDoc_DocumentTool.ShapeTool_s(doc.Main());label=tool.NewShape();trsf=gp_Trsf();trsf.SetTranslation(gp_Vec(100,200,300))
  tool.SetShape(label,BRepPrimAPI_MakeBox(10,20,30).Shape().Moved(TopLoc_Location(trsf)));return True
OCP.STEPCAFControl.STEPCAFControl_Reader=LocatedRootReader
try:
 spec=importlib.util.spec_from_file_location('cad_converter',converter);module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
 output=io.StringIO()
 with contextlib.redirect_stdout(output):module.convert('synthetic-located-root')
 result=json.loads(output.getvalue().splitlines()[-1]);assert result['bounds']==[[100,200,300],[110,220,330]],result
finally:OCP.STEPCAFControl.STEPCAFControl_Reader=original_reader
result,colours=run('/scripts/fixtures/cad/fidelity.step')
assert result['parts']==2 and result['bounds']==[[-55,-5,0],[100,20,35]],result
assert colours=={(1,0,0),(1,1,0),(0,0,1)},colours
with tempfile.TemporaryDirectory() as d:
 with tarfile.open('/fixtures/occt-import-js-0.0.23-source.tgz') as archive:
  for unit in ['mm','m','in']:
   source=archive.extractfile('package/test/testfiles/cube-units/cube-'+unit+'.step');path=Path(d)/'cube.step';path.write_bytes(source.read())
   result,_=run(path)
   for low,high in zip(*result['bounds']):assert abs(high-low-1000)<.0001,(unit,result)
print(json.dumps({'status':'pass','nestedAssemblyPlacement':True,'partAndFaceColours':True,'allFacesMeshed':True,'mmMetreInch':True,'finiteNormalsAndBatchBounds':True}))
'''
if __name__=='__main__':
    subprocess.run(['docker','run','--rm','--network','none','--memory','1g','--cpus','2','-e','PYTHONPATH=/opt/aide-cad','-e','LD_LIBRARY_PATH=/opt/aide-cad/system','-v',str(ROOT/'scripts')+':/scripts:ro','-v',str(ROOT/'internal/server/web/vendor/occt-import-js')+':/fixtures:ro','--entrypoint','python3',sys.argv[1] if len(sys.argv)>1 else 'aide:local','-c',PROGRAM],check=True,timeout=180)
