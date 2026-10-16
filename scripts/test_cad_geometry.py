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
# Mixed B-rep bodies, explicit construction wires and isolated points must all
# retain the same root transform. Pure wire/point STEP geometry is valid too.
from OCP.BRep import BRep_Builder
from OCP.TopoDS import TopoDS_Compound
from OCP.BRepBuilderAPI import BRepBuilderAPI_MakeEdge,BRepBuilderAPI_MakeVertex
from OCP.gp import gp_Pnt
class MixedGeometryReader(LocatedRootReader):
 with_body=True
 def Transfer(self,doc):
  shape=TopoDS_Compound();builder=BRep_Builder();builder.MakeCompound(shape)
  if self.with_body:builder.Add(shape,BRepPrimAPI_MakeBox(10,20,30).Shape())
  builder.Add(shape,BRepBuilderAPI_MakeEdge(gp_Pnt(20,0,0),gp_Pnt(20,10,0)).Edge())
  builder.Add(shape,BRepBuilderAPI_MakeVertex(gp_Pnt(0,0,40)).Vertex())
  tool=XCAFDoc_DocumentTool.ShapeTool_s(doc.Main());label=tool.NewShape();trsf=gp_Trsf();trsf.SetTranslation(gp_Vec(100,200,300));tool.SetShape(label,shape.Moved(TopLoc_Location(trsf)));return True
class WireGeometryReader(MixedGeometryReader):with_body=False
for reader,expected_triangles,expected_top in [(MixedGeometryReader,12,[120,220,340]),(WireGeometryReader,0,[120,210,340])]:
 OCP.STEPCAFControl.STEPCAFControl_Reader=reader
 try:
  output=io.StringIO()
  with contextlib.redirect_stdout(output):module.convert('synthetic-mixed-geometry')
  messages=[json.loads(line) for line in output.getvalue().splitlines()];complete=messages[-1]
  assert complete['triangles']==expected_triangles and complete['segments']==1 and complete['points']==1,complete
  assert complete['bounds']==[[100,200,300],expected_top],complete
  lines=next(item for item in messages if item.get('kind')=='native-lines');points=next(item for item in messages if item.get('kind')=='native-points')
  def world(item):
   coordinates=array.array('f');coordinates.frombytes(base64.b64decode(item['positions']));return [v+item['origin'][i%3] for i,v in enumerate(coordinates)]
  assert world(lines)==[120,200,300,120,210,300] and world(points)==[100,200,340]
 finally:OCP.STEPCAFControl.STEPCAFControl_Reader=original_reader
# Force the analytical-mesher failure on a synthetic trimmed torus. The
# replacement must remain a face on the original surface, not a missing part.
from OCP.Geom import Geom_ToroidalSurface
from OCP.gp import gp_Ax3
from OCP.BRepBuilderAPI import BRepBuilderAPI_MakeFace,BRepBuilderAPI_MakeVertex
from OCP.BRepAdaptor import BRepAdaptor_Surface
from OCP.BRepExtrema import BRepExtrema_DistShapeShape
from OCP.GeomAbs import GeomAbs_Torus
from OCP.TopAbs import TopAbs_FACE
import OCP.BRepMesh
original_mesher=OCP.BRepMesh.BRepMesh_IncrementalMesh
def analytical_failure(shape,*args):
 if shape.ShapeType()==TopAbs_FACE and BRepAdaptor_Surface(shape).GetType()==GeomAbs_Torus:return None
 return original_mesher(shape,*args)
torus=BRepBuilderAPI_MakeFace(Geom_ToroidalSurface(gp_Ax3(),1,2),0,.05,math.pi-.01,math.pi,1e-7).Face()
OCP.BRepMesh.BRepMesh_IncrementalMesh=analytical_failure
try:
 face,mesh,location,recovery=module.recover_face_mesh(torus)
 assert recovery=='nurbs' and mesh.NbTriangles()>0 and face.ShapeType()==TopAbs_FACE
 assert BRepAdaptor_Surface(torus).GetType()==GeomAbs_Torus,'original support geometry changed'
 for n in range(1,mesh.NbNodes()+1):
  point=mesh.Node(n).Transformed(location.Transformation())
  distance=BRepExtrema_DistShapeShape(BRepBuilderAPI_MakeVertex(point).Vertex(),torus)
  assert distance.IsDone() and distance.Value()<1e-6,'NURBS mesh vertex left original trimmed face'
finally:OCP.BRepMesh.BRepMesh_IncrementalMesh=original_mesher
# Zero-width trim is a boundary, even when its support has nonzero extent.
# A tiny but nonzero area must still mesh or fail explicitly.
from OCP.Geom import Geom_Plane
from OCP.gp import gp_Pln
zero_trim=BRepBuilderAPI_MakeFace(Geom_Plane(gp_Pln()),0,1,0,0,1e-7).Face()
finite_trim=BRepBuilderAPI_MakeFace(Geom_Plane(gp_Pln()),0,1,0,1e-8,1e-7).Face()
assert module.is_parametric_collapsed_face(zero_trim)
assert not module.is_parametric_collapsed_face(finite_trim)
OCP.BRepMesh.BRepMesh_IncrementalMesh=analytical_failure
try:
 assert not module.is_parametric_collapsed_face(torus)
finally:OCP.BRepMesh.BRepMesh_IncrementalMesh=original_mesher
# Only geometries wholly covered by every declared vertex tolerance qualify
# as collapsed; preserve their original boundary and report that distinction.
from OCP.BRepBuilderAPI import BRepBuilderAPI_MakePolygon
from OCP.TopExp import TopExp_Explorer
from OCP.TopAbs import TopAbs_VERTEX
from OCP.TopoDS import TopoDS
polygon=BRepBuilderAPI_MakePolygon();polygon.Add(gp_Pnt(0,0,0));polygon.Add(gp_Pnt(1,0,0));polygon.Add(gp_Pnt(0,1,0));polygon.Close()
triangle=BRepBuilderAPI_MakeFace(polygon.Wire()).Face()
assert not module.is_tolerance_collapsed_face(triangle),'ordinary finite face was classified as collapsed'
vertices=TopExp_Explorer(triangle,TopAbs_VERTEX);builder=BRep_Builder()
while vertices.More():builder.UpdateVertex(TopoDS.Vertex(vertices.Current()),10);vertices.Next()
assert module.is_tolerance_collapsed_face(triangle)
class CollapsedReader(LocatedRootReader):
 def Transfer(self,doc):
  tool=XCAFDoc_DocumentTool.ShapeTool_s(doc.Main());tool.SetShape(tool.NewShape(),triangle);return True
def refused_face_mesher(shape,*args):
 if shape.ShapeType()==TopAbs_FACE:return None
 return original_mesher(shape,*args)
OCP.STEPCAFControl.STEPCAFControl_Reader=CollapsedReader;OCP.BRepMesh.BRepMesh_IncrementalMesh=refused_face_mesher
try:
 output=io.StringIO()
 with contextlib.redirect_stdout(output):module.convert('synthetic-tolerance-collapsed-face')
 messages=[json.loads(line) for line in output.getvalue().splitlines()];complete=messages[-1]
 assert complete['kind']=='complete' and complete['collapsedFaces']==1 and complete['meshedFaces']==0 and complete['triangles']==0 and complete['segments']==3,complete
 assert any(item.get('kind')=='native-lines' for item in messages)
finally:
 OCP.STEPCAFControl.STEPCAFControl_Reader=original_reader;OCP.BRepMesh.BRepMesh_IncrementalMesh=original_mesher
result,colours=run('/scripts/fixtures/cad/fidelity.step')
assert result['parts']==2 and result['bounds']==[[-55,-5,0],[100,20,35]],result
assert colours=={(1,0,0),(1,1,0),(0,0,1)},colours
with tempfile.TemporaryDirectory() as d:
 with tarfile.open('/fixtures/occt-import-js-0.0.23-source.tgz') as archive:
  for unit in ['mm','m','in']:
   source=archive.extractfile('package/test/testfiles/cube-units/cube-'+unit+'.step');path=Path(d)/'cube.step';path.write_bytes(source.read())
   result,_=run(path)
   for low,high in zip(*result['bounds']):assert abs(high-low-1000)<.0001,(unit,result)
print(json.dumps({'status':'pass','nestedAssemblyPlacement':True,'partAndFaceColours':True,'allFacesMeshed':True,'nurbsFallbackRetainsTrimmedFace':True,'mixedAndPureWirePointGeometry':True,'collapsedFacesRetainBoundaryAndWarning':True,'zeroWidthTrimOnly':True,'mmMetreInch':True,'finiteNormalsAndBatchBounds':True}))
'''
if __name__=='__main__':
    subprocess.run(['docker','run','--rm','--network','none','--memory','1g','--cpus','2','-e','PYTHONPATH=/opt/aide-cad','-e','LD_LIBRARY_PATH=/opt/aide-cad/system','-v',str(ROOT/'scripts')+':/scripts:ro','-v',str(ROOT/'internal/server/web/vendor/occt-import-js')+':/fixtures:ro','--entrypoint','python3',sys.argv[1] if len(sys.argv)>1 else 'aide:local','-c',PROGRAM],check=True,timeout=180)
