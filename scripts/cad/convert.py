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
MAX_SEGMENTS = 500_000
MAX_POINTS = 200_000
BATCH_PRIMITIVES = 16_000

def emit(value):
    sys.stdout.write(json.dumps(value, separators=(',', ':'), allow_nan=False) + '\n')
    sys.stdout.flush()

def binary(values):
    if sys.byteorder != 'little':
        values.byteswap()
    return base64.b64encode(values.tobytes()).decode('ascii')

def is_tolerance_collapsed_face(face):
    """Require the whole exact bounding box inside EVERY vertex tolerance.

    Such a face is smaller than its own declared topological uncertainty. Keep
    its original edges/vertices, and report it; never discard ordinary faces
    merely because their mesher failed or their area looks small.
    """
    from OCP.Bnd import Bnd_Box
    from OCP.BRepBndLib import BRepBndLib
    from OCP.TopExp import TopExp_Explorer
    from OCP.TopAbs import TopAbs_VERTEX
    from OCP.TopoDS import TopoDS
    from OCP.BRep import BRep_Tool
    from itertools import product
    box = Bnd_Box()
    BRepBndLib.AddOptimal_s(face, box, False, False)
    if box.IsVoid() or box.IsWhole():
        return False
    low, high = box.CornerMin(), box.CornerMax()
    corners = list(product(*zip((low.X(), low.Y(), low.Z()), (high.X(), high.Y(), high.Z()))))
    iterator = TopExp_Explorer(face, TopAbs_VERTEX)
    if not iterator.More():
        return False
    while iterator.More():
        vertex = TopoDS.Vertex(iterator.Current())
        point = BRep_Tool.Pnt_s(vertex)
        center = (point.X(), point.Y(), point.Z())
        tolerance = BRep_Tool.Tolerance_s(vertex)
        if not math.isfinite(tolerance) or tolerance <= 0 or any(math.dist(center, corner) > tolerance for corner in corners):
            return False
        iterator.Next()
    return True

def is_parametric_collapsed_face(face):
    """A zero-width UV trim has no surface interior; retain its boundaries.

    Require an exactly collapsed finite parameter interval AND exactly zero
    integrated area. Small areas, narrow but nonzero trims, or mesher failures
    alone never qualify. The supporting surface may still have a large extent.
    """
    from OCP.BRepTools import BRepTools
    from OCP.BRepGProp import BRepGProp
    from OCP.GProp import GProp_GProps
    bounds = BRepTools.UVBounds_s(face)
    if not all(math.isfinite(value) for value in bounds):
        return False
    if bounds[0] != bounds[1] and bounds[2] != bounds[3]:
        return False
    properties = GProp_GProps()
    BRepGProp.SurfaceProperties_s(face, properties)
    return properties.Mass() == 0

def recover_face_mesh(face):
    """Retain the face: retry finer sampling, then an OCCT NURBS copy.

    Some narrow valid trimmed surfaces fail the analytical mesher at ordinary
    deflection. Converting their support geometry avoids that parameterization
    failure; it does not replace the face with a box or omit its boundaries.
    """
    from OCP.BRep import BRep_Tool
    from OCP.BRepTools import BRepTools
    from OCP.BRepMesh import BRepMesh_IncrementalMesh
    from OCP.BRepBuilderAPI import BRepBuilderAPI_NurbsConvert
    from OCP.BRepCheck import BRepCheck_Analyzer
    from OCP.TopoDS import TopoDS
    from OCP.TopLoc import TopLoc_Location

    def attempt(candidate):
        for deflection in (.1, .01, .001, .0001):
            BRepTools.Clean_s(candidate)
            BRepMesh_IncrementalMesh(candidate, deflection, False, .45, False)
            location = TopLoc_Location()
            mesh = BRep_Tool.Triangulation_s(candidate, location)
            if mesh and mesh.NbTriangles() > 0:
                return mesh, location
        return None

    recovered = attempt(face)
    if recovered:
        return face, *recovered, 'refined'
    converted = TopoDS.Face(BRepBuilderAPI_NurbsConvert(face, True).Shape())
    if not converted.IsNull() and BRepCheck_Analyzer(converted).IsValid():
        recovered = attempt(converted)
        if recovered:
            return converted, *recovered, 'nurbs'
    if is_tolerance_collapsed_face(face) or is_parametric_collapsed_face(face):
        return face, None, None, 'collapsed'
    raise ValueError('存在无法生成完整网格的 CAD 面；细化采样和 NURBS 转换均未成功，未输出不完整模型。')

def convert(path):
    from OCP.STEPCAFControl import STEPCAFControl_Reader
    from OCP.TDocStd import TDocStd_Document
    from OCP.TCollection import TCollection_ExtendedString
    from OCP.XCAFDoc import XCAFDoc_DocumentTool
    from OCP.XCAFPrs import XCAFPrs_DocumentExplorer, XCAFPrs
    from OCP.collections import IndexedDataMap_TopoDS_Shape_XCAFPrs_Style_TopTools_ShapeMapHasher
    from OCP.BRepMesh import BRepMesh_IncrementalMesh
    from OCP.TopExp import TopExp_Explorer
    from OCP.TopAbs import TopAbs_FACE, TopAbs_SOLID, TopAbs_REVERSED, TopAbs_EDGE, TopAbs_VERTEX
    from OCP.TopoDS import TopoDS
    from OCP.BRep import BRep_Tool
    from OCP.TopLoc import TopLoc_Location
    from OCP.Interface import Interface_Static
    from OCP.Quantity import Quantity_TOC_RGB
    from OCP.BRepAdaptor import BRepAdaptor_Curve
    from OCP.GCPnts import GCPnts_QuasiUniformDeflection

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
    recovered_faces = nurbs_faces = empty_nodes = collapsed_faces = 0
    lower = [math.inf]*3
    upper = [-math.inf]*3
    positions, normals, colors = array.array('f'), array.array('f'), array.array('f')
    origin = None
    batch_parts = 0
    part_ranges = []
    instance_id = 0
    current_part = current_name = None

    def flush():
        nonlocal positions, normals, colors, origin, batch_parts, part_ranges
        if not positions:
            return
        emit(dict(kind='native-mesh', positions=binary(positions), normals=binary(normals), colors=binary(colors), origin=origin, sourceCount=batch_parts, parts=part_ranges))
        positions, normals, colors = array.array('f'), array.array('f'), array.array('f')
        origin = None
        batch_parts = 0
        part_ranges = []

    def track_part(ranges, start, count):
        if ranges and ranges[-1]["id"] == current_part:
            ranges[-1]["count"] += count
        else:
            ranges.append(dict(id=current_part, name=current_name, start=start, count=count))

    primitive_counts = {'native-lines': 0, 'native-points': 0}
    primitive_buffers = {kind: [array.array('f'), array.array('f'), None, []] for kind in primitive_counts}

    def flush_primitive(kind):
        buffer = primitive_buffers[kind]
        if buffer[0]:
            emit(dict(kind=kind, positions=binary(buffer[0]), colors=binary(buffer[1]), origin=buffer[2], sourceCount=1, parts=buffer[3]))
            primitive_buffers[kind] = [array.array('f'), array.array('f'), None, []]

    def primitive(kind, vertices, color):
        primitive_counts[kind] += 1
        if primitive_counts[kind] > (MAX_SEGMENTS if kind == 'native-lines' else MAX_POINTS):
            raise ValueError('模型超过线或点几何预算，未输出不完整模型。')
        buffer = primitive_buffers[kind]
        if buffer[2] is None:
            buffer[2] = vertices[0]
        track_part(buffer[3], len(buffer[0])//3, len(vertices))
        for xyz in vertices:
            if not all(math.isfinite(v) for v in xyz):
                raise ValueError('CAD 几何包含无效坐标。')
            for axis in range(3):
                lower[axis] = min(lower[axis], xyz[axis])
                upper[axis] = max(upper[axis], xyz[axis])
                buffer[0].append(xyz[axis]-buffer[2][axis])
            buffer[1].extend(color)
        stride = 6 if kind == 'native-lines' else 3
        if len(buffer[0]) >= BATCH_PRIMITIVES*stride:
            flush_primitive(kind)

    while explorer.More():
        node = explorer.Current()
        if node.IsAssembly:
            explorer.Next()
            continue
        if instance_id >= 50000:
            raise ValueError('模型超过五万个零件实例预算。')
        current_part = 'native-' + str(instance_id)
        instance_id += 1
        current_name = current_part
        from OCP.TDataStd import TDataStd_Name
        for label in (node.Label, node.RefLabel):
            if label.IsNull():
                continue
            if not label.IsAttribute(TDataStd_Name.GetID_s()):
                continue
            attribute = TDataStd_Name()
            if label.FindAttribute(TDataStd_Name.GetID_s(), attribute):
                candidate = attribute.Get().ToExtString()[:160]
                if candidate and not candidate.startswith('=>['):
                    current_name = candidate
                    break
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
        collapsed_shapes = []
        part_start_faces = faces
        part_start_primitives = sum(primitive_counts.values())
        face_iter = TopExp_Explorer(shape, TopAbs_FACE)
        while face_iter.More():
            face = TopoDS.Face(face_iter.Current())
            source_face = face
            loc = TopLoc_Location()
            mesh = BRep_Tool.Triangulation_s(face, loc)
            if not mesh or mesh.NbTriangles() == 0:
                # Keep every nonempty face, including open shells and narrow
                # trimmed surfaces that require a finer or NURBS meshing path.
                face, mesh, loc, recovery = recover_face_mesh(face)
                recovered_faces += 1
                nurbs_faces += recovery == 'nurbs'
                if recovery == 'collapsed':
                    collapsed_faces += 1
                    collapsed_shapes.append(source_face)
                    faces += 1
                    face_iter.Next()
                    continue
            if not mesh.HasNormals():
                mesh.ComputeNormals()
            trsf = node.Location.Multiplied(loc).Transformation()
            color = default_color
            styled_face = source_face.Moved(prototype_location)
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
                track_part(part_ranges, len(positions)//3, 3)
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
        # Preserve explicit wire/point geometry without drawing every surface
        # boundary twice. The curve adaptor already applies the edge location.
        curve_color = style.GetColorCurv().Values(Quantity_TOC_RGB) if style.IsSetColorCurv() else default_color
        placement = node.Location.Transformation()
        edge_groups = [(TopExp_Explorer(shape, TopAbs_EDGE, TopAbs_FACE), curve_color, None)]
        for collapsed in collapsed_shapes:
            retained_color = curve_color
            original_style = collapsed.Moved(prototype_location)
            if styles.Contains(original_style) and styles.FindFromKey(original_style).IsSetColorSurf():
                retained_color = styles.FindFromKey(original_style).GetColorSurf().Values(Quantity_TOC_RGB)
            edge_groups.append((TopExp_Explorer(collapsed, TopAbs_EDGE), retained_color, collapsed))
        for edge_iter, retained_color, collapsed in edge_groups:
            before_lines = primitive_counts['native-lines']
            while edge_iter.More():
                edge = TopoDS.Edge(edge_iter.Current())
                if not BRep_Tool.Degenerated_s(edge):
                    curve = BRepAdaptor_Curve(edge)
                    samples = GCPnts_QuasiUniformDeflection(curve, .1)
                    if not samples.IsDone() or samples.NbPoints() < 2:
                        raise ValueError('CAD 曲线无法完整离散，未输出不完整模型。')
                    edge_color = retained_color
                    styled_edge = edge.Moved(prototype_location)
                    if styles.Contains(styled_edge):
                        edge_style = styles.FindFromKey(styled_edge)
                        if edge_style.IsSetColorCurv():
                            edge_color = edge_style.GetColorCurv().Values(Quantity_TOC_RGB)
                    previous = None
                    for i in range(1, samples.NbPoints()+1):
                        point = samples.Value(i).Transformed(placement)
                        xyz = (point.X(), point.Y(), point.Z())
                        if previous is not None:
                            primitive('native-lines', (previous, xyz), edge_color)
                        previous = xyz
                edge_iter.Next()
            if collapsed is not None and primitive_counts['native-lines'] == before_lines:
                retained_vertices = TopExp_Explorer(collapsed, TopAbs_VERTEX)
                while retained_vertices.More():
                    point = BRep_Tool.Pnt_s(TopoDS.Vertex(retained_vertices.Current())).Transformed(placement)
                    primitive('native-points', ((point.X(), point.Y(), point.Z()),), retained_color)
                    retained_vertices.Next()
        vertex_iter = TopExp_Explorer(shape, TopAbs_VERTEX, TopAbs_EDGE)
        while vertex_iter.More():
            point = BRep_Tool.Pnt_s(TopoDS.Vertex(vertex_iter.Current())).Transformed(placement)
            primitive('native-points', ((point.X(), point.Y(), point.Z()),), curve_color)
            vertex_iter.Next()
        if faces == part_start_faces and sum(primitive_counts.values()) == part_start_primitives:
            empty_nodes += 1
            explorer.Next()
            continue
        parts += 1
        batch_parts += 1
        for kind, buffer in primitive_buffers.items():
            if len(buffer[3]) >= 128:
                flush_primitive(kind)
        if parts % 128 == 0:
            flush()
            emit(dict(kind='progress', phase='tessellate', parts=parts, faces=faces, triangles=total))
        explorer.Next()
    flush()
    for kind in primitive_buffers:
        flush_primitive(kind)
    if total == 0 and not sum(primitive_counts.values()):
        raise ValueError('文件中没有可显示的 STEP 曲面。')
    emit(dict(kind='complete', engine='occt-native', triangles=total, segments=primitive_counts['native-lines'], points=primitive_counts['native-points'], emptyNodes=empty_nodes, parts=parts, faces=faces, meshedFaces=faces-collapsed_faces, collapsedFaces=collapsed_faces, bounds=[lower,upper], unit='mm', linearDeflection=.1, recoveredFaces=recovered_faces, nurbsFaces=nurbs_faces, seconds=time.monotonic()-start))

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
