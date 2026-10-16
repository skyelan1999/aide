#!/usr/bin/env python3
"""Isolated STEP -> bounded mesh batches. Original STEP is never modified.

OpenCascade keeps exact B-rep until process exit. Release the exchange reader
before tessellation; never mesh a whole assembly or collect all output in RAM.
stdout is NDJSON protocol; OCCT diagnostics go to stderr.
"""
import array
import base64
import ctypes
import gc
import json
import math
import os
import sys
import time

MAX_TRIANGLES = 3_000_000
BATCH_TRIANGLES = 16_000

def emit(value):
    sys.stdout.write(json.dumps(value, separators=(',', ':'), allow_nan=False) + '\n')
    sys.stdout.flush()

def binary(values):
    if sys.byteorder != 'little':
        values.byteswap()
    return base64.b64encode(values.tobytes()).decode('ascii')

def convert(path):
    from OCP.STEPCAFControl import STEPCAFControl_Reader
    from OCP.TDocStd import TDocStd_Document
    from OCP.TCollection import TCollection_ExtendedString
    from OCP.XCAFDoc import XCAFDoc_DocumentTool
    from OCP.XCAFPrs import XCAFPrs_DocumentExplorer, XCAFPrs
    from OCP.collections import IndexedDataMap_TopoDS_Shape_XCAFPrs_Style_TopTools_ShapeMapHasher
    from OCP.BRepMesh import BRepMesh_IncrementalMesh
    from OCP.TopExp import TopExp_Explorer
    from OCP.TopAbs import TopAbs_FACE, TopAbs_SOLID, TopAbs_REVERSED
    from OCP.TopoDS import TopoDS
    from OCP.BRep import BRep_Tool
    from OCP.TopLoc import TopLoc_Location
    from OCP.Interface import Interface_Static
    from OCP.Quantity import Quantity_TOC_RGB

    # OCCT's process-global settings are isolated to this one conversion child.
    Interface_Static.SetCVal_s('xstep.cascade.unit', 'MM')
    start = time.monotonic()
    emit(dict(kind='progress', phase='parse', engine='occt-native', unit='mm'))
    doc = TDocStd_Document(TCollection_ExtendedString('AIDE CAD'))
    reader = STEPCAFControl_Reader()
    reader.SetColorMode(True)
    reader.SetNameMode(True)
    reader.SetLayerMode(False)
    if int(reader.ReadFile(path)) != 1 or not reader.Transfer(doc):
        raise ValueError('STEP 几何转换失败，文件可能不完整或包含不支持的实体。')
    del reader
    gc.collect()
    ctypes.CDLL('libc.so.6').malloc_trim(0)
    emit(dict(kind='progress', phase='tessellate', seconds=time.monotonic()-start))
    tool = XCAFDoc_DocumentTool.ShapeTool_s(doc.Main())
    explorer = XCAFPrs_DocumentExplorer(doc, 0)
    total = parts = faces = 0
    lower = [math.inf]*3
    upper = [-math.inf]*3
    positions, normals, colors = array.array('f'), array.array('f'), array.array('f')
    origin = None
    batch_parts = 0

    def flush():
        nonlocal positions, normals, colors, origin, batch_parts
        if not positions:
            return
        emit(dict(kind='native-mesh', positions=binary(positions), normals=binary(normals), colors=binary(colors), origin=origin, sourceCount=batch_parts))
        positions, normals, colors = array.array('f'), array.array('f'), array.array('f')
        origin = None
        batch_parts = 0

    while explorer.More():
        node = explorer.Current()
        if node.IsAssembly:
            explorer.Next()
            continue
        shape = tool.GetShape_s(node.RefLabel)
        if shape.IsNull():
            raise ValueError('装配包含无法读取的零件，未输出不完整模型。')
        prototype_location = shape.Location()
        shape = shape.Located(TopLoc_Location())
        styles = IndexedDataMap_TopoDS_Shape_XCAFPrs_Style_TopTools_ShapeMapHasher()
        XCAFPrs.CollectStyleSettings_s(node.RefLabel, TopLoc_Location(), styles)
        style = node.Style
        default_color = style.GetColorSurf().Values(Quantity_TOC_RGB) if style.IsSetColorSurf() else (.615, .749, .89)
        # Mesh individual solids; a compound with tens of thousands of solids must
        # never be submitted to the mesher as one whole allocation graph.
        solids = TopExp_Explorer(shape, TopAbs_SOLID)
        while solids.More():
            BRepMesh_IncrementalMesh(solids.Current(), .1, False, .45, False)
            solids.Next()
        part_start_faces = faces
        face_iter = TopExp_Explorer(shape, TopAbs_FACE)
        while face_iter.More():
            face = TopoDS.Face(face_iter.Current())
            loc = TopLoc_Location()
            mesh = BRep_Tool.Triangulation_s(face, loc)
            if not mesh:
                # Open shells and single surfaces are valid STEP geometry too.
                BRepMesh_IncrementalMesh(face, .1, False, .45, False)
                mesh = BRep_Tool.Triangulation_s(face, loc)
            if not mesh or mesh.NbTriangles() == 0:
                raise ValueError('存在未生成曲面的 CAD 面，未输出不完整模型。')
            if not mesh.HasNormals():
                mesh.ComputeNormals()
            trsf = node.Location.Multiplied(loc).Transformation()
            color = default_color
            styled_face = face.Moved(prototype_location)
            if styles.Contains(styled_face):
                face_style = styles.FindFromKey(styled_face)
                if face_style.IsSetColorSurf():
                    color = face_style.GetColorSurf().Values(Quantity_TOC_RGB)
            reverse = face.Orientation() == TopAbs_REVERSED
            reverse_winding = reverse != trsf.IsNegative()
            for i in range(1, mesh.NbTriangles()+1):
                total += 1
                if total > MAX_TRIANGLES:
                    raise ValueError('模型超过三百万三角面预算，未完成的模型已丢弃。')
                indices = list(mesh.Triangle(i).Get())
                if reverse_winding:
                    indices[1], indices[2] = indices[2], indices[1]
                for index in indices:
                    p = mesh.Node(index).Transformed(trsf)
                    xyz = (p.X(), p.Y(), p.Z())
                    if not all(math.isfinite(v) for v in xyz):
                        raise ValueError('CAD 几何包含无效坐标。')
                    if origin is None:
                        origin = xyz
                    for axis in range(3):
                        lower[axis] = min(lower[axis], xyz[axis])
                        upper[axis] = max(upper[axis], xyz[axis])
                        positions.append(xyz[axis]-origin[axis])
                    n = mesh.Normal(index).Transformed(trsf)
                    normals.extend(((-n.X(), -n.Y(), -n.Z()) if reverse else (n.X(), n.Y(), n.Z())))
                    colors.extend(color)
                if len(positions)//9 >= BATCH_TRIANGLES:
                    flush()
            faces += 1
            if faces % 5000 == 0:
                emit(dict(kind='progress', phase='tessellate', parts=parts, faces=faces, triangles=total))
            face_iter.Next()
        if faces == part_start_faces:
            raise ValueError('装配包含只有线或点的零件，当前无法生成完整曲面预览。')
        parts += 1
        batch_parts += 1
        if parts % 128 == 0:
            flush()
            emit(dict(kind='progress', phase='tessellate', parts=parts, faces=faces, triangles=total))
        explorer.Next()
    flush()
    if total == 0:
        raise ValueError('文件中没有可显示的 STEP 曲面。')
    emit(dict(kind='complete', engine='occt-native', triangles=total, parts=parts, faces=faces, bounds=[lower,upper], unit='mm', linearDeflection=.1, seconds=time.monotonic()-start))

if __name__ == '__main__':
    # Exchange readers can write diagnostics to C++ stdout, corrupting NDJSON.
    # Keep protocol on a duplicated descriptor and redirect native stdout.
    protocol = os.dup(1)
    os.dup2(2, 1)
    sys.stdout = os.fdopen(protocol, 'w', buffering=1)
    try:
        convert(sys.argv[1])
    except Exception as exc:
        emit(dict(error=str(exc)[:1000]))
        sys.exit(1)
