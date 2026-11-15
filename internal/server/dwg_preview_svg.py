"""Bounded local DWG-derived DXF rendering. Embedded in the Go executable."""
import collections
import json
import logging
import math
import sys
from pathlib import Path

import ezdxf
from ezdxf.addons.drawing import Frontend, RenderContext, config, layout, svg
from ezdxf.addons.drawing.properties import Filling
from ezdxf.addons.drawing.frontend import closed_loops, ignore_text_boxes
from ezdxf.fonts import fonts
from ezdxf.acis import api as acis
from ezdxf.acis.entities import Curve, Surface, register
from ezdxf.math import Vec3, ConstructionEllipse, cubic_bezier_from_ellipse
from ezdxf.path import Path as CADPath, add_bezier4p

MAX_ENTITIES = 50000
MAX_SOURCE_OBJECTS = 100000  # Includes unused dictionaries and block definitions.
MAX_POINTS = 2000000  # Dense CAD dash patterns; still bounded by 512 MiB / 15 CPU s.
MAX_BYTES = 16 << 20


class PreviewLimit(Exception):
    pass


@register
class PreviewPlaneSurface(Surface):
    type = 'plane-surface'

    def restore_data(self, loader):
        self.origin = Vec3(loader.read_vec3())
        self.normal = Vec3(loader.read_vec3())
        self.u_axis = Vec3(loader.read_vec3())
        self.reverse_v = loader.read_bool("reverse_v", "forward_v")
        super().restore_data(loader)


@register
class PreviewEllipseCurve(Curve):
    type = 'ellipse-curve'

    def restore_data(self, loader):
        self.center = Vec3(loader.read_vec3())
        self.normal = Vec3(loader.read_vec3())
        self.major = Vec3(loader.read_vec3())
        self.ratio = loader.read_double()
        super().restore_data(loader)

    def evaluate(self, param):
        minor = self.normal.cross(self.major).normalize(self.major.magnitude * self.ratio)
        return self.center + self.major * math.cos(param) + minor * math.sin(param)


def acis_chain(first, following, circular=False):
    node, seen = first, set()
    while not node.is_none:
        if id(node) in seen:
            if circular and node is first:
                return
            raise ValueError('invalid ACIS topology cycle')
        if len(seen) >= 4096:
            raise PreviewLimit('ACIS topology budget')
        seen.add(id(node))
        yield node
        node = getattr(node, following)
    if circular:
        raise ValueError('open ACIS loop')


def region_paths(entity):
    """Evaluate supported planar B-rep loops; reject incomplete topology.

    REGION wireframe preview uses every original boundary, including holes.
    Elliptic edges stay cubic curves, never vertex-only replacement chords.
    """
    data = entity.sab if entity.has_binary_data else entity.sat
    if not data:
        raise ValueError('missing ACIS data')
    result, edge_count = [], 0
    for body in acis.load(data):
        if not body.wire.is_none:
            raise ValueError('unsupported ACIS wire')
        for lump in acis_chain(body.lump, 'next_lump'):
            for shell in acis_chain(lump.shell, 'next_shell'):
                for face in acis_chain(shell.face, 'next_face'):
                    if not isinstance(face.surface, PreviewPlaneSurface):
                        raise ValueError('unsupported ACIS surface')
                    for loop in acis_chain(face.loop, 'next_loop'):
                        path = None
                        for coedge in acis_chain(loop.coedge, 'next_coedge', circular=True):
                            edge_count += 1
                            if edge_count > 50000:
                                raise PreviewLimit('ACIS edge budget')
                            edge = coedge.edge
                            start, end = edge.start_vertex.point.location, edge.end_vertex.point.location
                            if not valid_points([start, end]):
                                raise ValueError('invalid ACIS vertex')
                            if edge.curve.type == 'straight-curve':
                                segment = CADPath(start); segment.line_to(end)
                            elif isinstance(edge.curve, PreviewEllipseCurve):
                                curve, a, b = edge.curve, edge.start_param, edge.end_param
                                if (not valid_points([curve.center, curve.normal, curve.major])
                                        or not 0 < curve.ratio <= 1
                                        or not math.isfinite(a + b)
                                        or abs(b-a) > math.tau + 1e-8 or abs(b-a) < 1e-12):
                                    raise ValueError('invalid ACIS ellipse')
                                ellipse = ConstructionEllipse(curve.center, curve.major, curve.normal,
                                                              curve.ratio, min(a, b), max(a, b))
                                curves = list(cubic_bezier_from_ellipse(ellipse))
                                segment = CADPath(curves[0].control_points[0]); add_bezier4p(segment, curves)
                                if b < a:
                                    segment = segment.reversed()
                                if segment.start.distance(start) > 1e-5 or segment.end.distance(end) > 1e-5:
                                    if segment.end.distance(start) < 1e-5 and segment.start.distance(end) < 1e-5:
                                        segment = segment.reversed()
                                    else:
                                        raise ValueError('ACIS curve/vertex mismatch')
                            else:
                                raise ValueError('unsupported ACIS curve')
                            if coedge.sense:
                                segment = segment.reversed()
                            if path is None:
                                path = segment
                            else:
                                if path.end.distance(segment.start) > 1e-5:
                                    raise ValueError('disconnected ACIS loop')
                                path.append_path(segment)
                        if path is None or path.end.distance(path.start) > 1e-5:
                            raise ValueError('incomplete ACIS loop')
                        if not body.transform.is_none:
                            path = path.transform(body.transform.matrix)
                        matrix = entity._temporary_transformation.get_matrix()
                        if matrix is not None:
                            path = path.transform(matrix)
                        result.append(path)
    if not result:
        raise ValueError('empty ACIS body')
    return result


def invalid_hatch_scale(entity):
    return (entity.dxftype() in ('HATCH', 'MPOLYGON') and entity.pattern is not None
            and (not math.isfinite(entity.dxf.pattern_scale) or abs(entity.dxf.pattern_scale) < 1e-12))


class CADContext(RenderContext):
    def resolve_filling(self, entity):
        # A malformed pattern scale must not prevent every other entity opening.
        if invalid_hatch_scale(entity):
            return Filling()
        return super().resolve_filling(entity)


def valid_points(points):
    return all(all(math.isfinite(v) and abs(v) <= 1e12 for v in point) for point in points)


def discard_invalid_proxies(source, destination):
    """Drop only opaque proxy records with invalid binary chunks, never fix geometry."""
    result, record = bytearray(), bytearray()
    kind, invalid, dropped = b'', False, 0
    with open(source, 'rb') as stream:
        while True:
            code, value = stream.readline(), stream.readline()
            if not code:
                if invalid:
                    dropped += 1
                else:
                    result.extend(record)
                break
            try:
                tag = int(code.strip())
            except ValueError:
                raise ValueError('invalid DXF tag')
            if tag == 0:
                if invalid:
                    dropped += 1
                else:
                    result.extend(record)
                record, kind, invalid = bytearray(), value.strip(), False
            if kind == b'ACAD_PROXY_ENTITY' and 310 <= tag <= 319:
                text = value.strip()
                if len(text) % 2 or any(v not in b'0123456789abcdefABCDEF' for v in text):
                    invalid = True
            record.extend(code); record.extend(value)
    if not dropped:
        raise ValueError('no recoverable proxy records')
    Path(destination).write_bytes(result)
    return dropped


def normalize_empty_dictionary_entries(data):
    """Remove only empty-key/null-handle pairs that shift ezdxf dictionary keys.

    LibreDWG can emit these placeholders between valid entries. All named
    references, source objects, and geometry bytes remain unchanged.
    """
    lines = data.splitlines(keepends=True)
    result, kind, count, i = bytearray(), b'', 0, 0
    while i + 1 < len(lines):
        code, value = lines[i:i+2]
        if code.strip() == b'0':
            kind = value.strip()
        if (kind in (b'DICTIONARY', b'ACDBDICTIONARYWDFLT')
                and code.strip() == b'3' and not value.strip()
                and i + 3 < len(lines) and lines[i+2].strip() in (b'350', b'360')
                and lines[i+3].strip() == b'0'):
            count += 1
            i += 4
            continue
        result.extend(code); result.extend(value)
        i += 2
    if i < len(lines):
        result.extend(lines[i])
    return bytes(result), count


class BoundedSVG(svg.SVGBackend):
    def __init__(self):
        super().__init__()
        self.point_count = 0
        self.record_count = 0

    def store(self, record, properties):
        # Invisible MTEXT background polygons can carry huge fill-scale values.
        # They must not inflate the automatic fit box or consume geometry work.
        if len(properties.color) == 9 and properties.color[-2:] == '00':
            return
        self.record_count += 1
        if self.record_count > MAX_ENTITIES:
            raise PreviewLimit('node budget')
        shapes = []
        for name in ('path', 'points', 'lines'):
            value = getattr(record, name, None)
            if value is not None:
                shapes.append(value)
        shapes.extend(getattr(record, 'paths', ()))
        for shape in shapes:
            vertices = shape.np_vertices()
            self.point_count += len(vertices)
            if self.point_count > MAX_POINTS:
                raise PreviewLimit('point budget')
            if not valid_points(vertices):
                raise PreviewLimit('invalid coordinates')
        super().store(record, properties)


class CADFrontend(Frontend):
    def __init__(self, ctx, out):
        self.depth = 0
        self.text_chars = 0
        self.visits = 0
        self.skipped = collections.Counter()
        self.rendered = collections.Counter()
        self.degraded = collections.Counter()
        self.recovered = collections.Counter()
        self.messages = 0
        self.snap_points = set()
        super().__init__(ctx, out, config.Configuration(
            background_policy=config.BackgroundPolicy.OFF,
            lineweight_policy=config.LineweightPolicy.RELATIVE,
            image_policy=config.ImagePolicy.IGNORE,
            # Pattern generation is bounded independently of the process limit.
            hatching_timeout=1.0, min_hatch_line_distance=0.1,
            max_flattening_distance=0.1))
        self.messages = 0

    def skip_entity(self, entity, msg):
        # Hidden layers and invisible ATTDEFs are intentionally absent in CAD.
        if 'invisible' not in msg.lower():
            self.skipped[entity.dxftype()] += 1

    def log_message(self, message):
        # Library diagnostics may contain customer content; return counts only.
        self.messages += 1

    def draw_text_entity(self, entity, properties):
        # Some CAD applications intentionally store metadata TEXT at 1e-29.
        # Keep the original height instead of skipping it or enlarging a label.
        if entity.dxftype() == 'TEXT' and 0 < entity.dxf.height < 1e-9:
            from ezdxf.addons.drawing.frontend import is_spatial_text
            if is_spatial_text(Vec3(entity.dxf.extrusion)):
                return self.draw_text_entity_3d(entity, properties)
            return self.draw_text_entity_2d(entity, properties)
        return super().draw_text_entity(entity, properties)

    def draw_mtext_entity(self, entity, properties):
        from ezdxf.addons.drawing.frontend import is_complex_mtext, is_spatial_text
        from ezdxf.tools.text_layout import LayoutError
        if (entity.dxf.width or 0) <= 0 and not entity.all_columns_plain_text().strip():
            # Formatting-only, zero-width dimension labels have no glyphs or
            # rectangular background to draw. Do not invent text or a box.
            self.recovered['MTEXT_EMPTY'] += 1
            return
        if is_spatial_text(Vec3(entity.dxf.extrusion)) or not is_complex_mtext(entity):
            return super().draw_mtext_entity(entity, properties)
        backend = self.pipeline.backend
        before = len(backend.records), backend.point_count, backend.record_count
        try:
            self.draw_complex_mtext(entity, properties)
            return
        except LayoutError:
            # Remove any incomplete attempt before a single bounded retry.
            del backend.records[before[0]:]
            backend.point_count, backend.record_count = before[1:]
        columns = entity.columns
        if columns is not None and columns.count == 1:
            # Substituted font line metrics may exceed a fixed single-column
            # height. Keep text, formatting, width, height and insertion point;
            # use auto column height instead of silently dropping the label.
            copy = entity.copy()
            copy._columns = None
            try:
                self.draw_complex_mtext(copy, properties)
                self.recovered['MTEXT_AUTO_HEIGHT'] += 1
                self.messages += 1
                return
            except LayoutError:
                del backend.records[before[0]:]
                backend.point_count, backend.record_count = before[1:]
        self.skip_entity(entity, 'invalid MTEXT layout')

    def draw_region_entity(self, entity, properties):
        try:
            paths = region_paths(entity)
        except (ValueError, TypeError, acis.AcisException):
            self.skip_entity(entity, 'unsupported or invalid ACIS boundary')
            return
        for path in paths:
            self.pipeline.draw_path(path, properties)

    def draw_hatch_entity(self, entity, properties, *, loops=None):
        if not invalid_hatch_scale(entity):
            return super().draw_hatch_entity(entity, properties, loops=loops)
        # Recover original boundaries, including holes, without inventing a
        # pattern scale or substituting a misleading solid engineering fill.
        paths = loops if loops is not None else closed_loops(
            list(entity.paths.rendering_paths(entity.dxf.hatch_style)),
            entity.ocs(), entity.dxf.elevation.z)
        for path in ignore_text_boxes(paths):
            self.pipeline.draw_path(path, properties)
        self.degraded[entity.dxftype()] += 1

    def draw_entity(self, entity, properties):
        self.depth += 1
        try:
            if self.depth > 32:
                raise PreviewLimit('block depth budget')
            self._draw_cad_entity(entity, properties)
        finally:
            self.depth -= 1

    def _draw_cad_entity(self, entity, properties):
        if entity.dxftype() in ('TEXT', 'MTEXT', 'ATTRIB', 'ATTDEF'):
            label = entity.text if entity.dxftype() == 'MTEXT' else entity.dxf.get('text', '')
            self.text_chars += len(label)
            if len(label) > 16384 or self.text_chars > 1 << 20:
                raise PreviewLimit('text budget')
        self.visits += 1
        if self.visits > MAX_ENTITIES:
            raise PreviewLimit('entity expansion budget')
        if entity.dxftype() == 'SPLINE':
            controls, fits = list(entity.control_points), list(entity.fit_points)
            # Control points + knots define the curve. Do not draw the control
            # polygon or manufacture points when the converter lost the data.
            points = controls or fits
            if not points or not valid_points(points):
                self.skipped['SPLINE'] += 1
                return
            if controls:
                knots = list(entity.knots)
                degree = entity.dxf.degree
                weights = list(entity.weights)
                if (degree < 1 or degree > 16 or len(knots) != len(controls) + degree + 1
                        or any(not math.isfinite(v) for v in knots)
                        or any(a > b for a, b in zip(knots, knots[1:]))
                        or (weights and (len(weights) != len(controls)
                            or any(not math.isfinite(v) or v <= 0 for v in weights)))):
                    self.skipped['SPLINE'] += 1
                    return
                entity.fit_points = []
            for key in ('start_tangent', 'end_tangent'):
                if entity.dxf.hasattr(key) and not valid_points([entity.dxf.get(key)]):
                    entity.dxf.discard(key)
        before = len(self.pipeline.backend.records)
        if entity.dxftype() == 'REGION':
            self.pipeline.enter_entity(entity, properties)
            if not entity.is_virtual:
                self.pipeline.set_current_entity_handle(entity.dxf.handle)
            self.draw_region_entity(entity, properties)
            self.pipeline.exit_entity(entity)
        else:
            super().draw_entity(entity, properties)
        if len(self.pipeline.backend.records) > before:
            self.rendered[entity.dxftype()] += 1
            self.collect_snaps(entity)
        elif entity.dxftype() == 'ACAD_PROXY_ENTITY':
            self.skipped['ACAD_PROXY_ENTITY'] += 1

    def collect_snaps(self, entity):
        # Visible, evaluated entities only; block expansion already supplies WCS.
        # Control polygons and font outlines are not selectable geometry.
        kind = entity.dxftype()
        points = []
        if kind == 'LINE':
            points = [entity.dxf.start, entity.dxf.end]
        elif kind in ('CIRCLE', 'ARC'):
            points = [entity.ocs().to_wcs(entity.dxf.center)]
            if kind == 'ARC':
                points.extend([entity.start_point, entity.end_point])
        elif kind in ('LWPOLYLINE', 'POLYLINE'):
            points = entity.vertices_in_wcs() if kind == 'LWPOLYLINE' else entity.points_in_wcs()
        elif kind in ('SPLINE', 'ELLIPSE'):
            curve = entity.construction_tool()
            if kind == 'SPLINE':
                points = [curve.point(0), curve.point(curve.max_t)]
            else:
                points = [curve.start_point, curve.end_point, curve.center]
        for point in points:
            if len(self.snap_points) >= 25000:
                break
            if valid_points([point]):
                self.snap_points.add((float(point[0]), float(point[1])))


def render(source, destination):
    logging.disable(logging.CRITICAL)
    if Path(source).stat().st_size > MAX_BYTES:
        raise PreviewLimit('input budget')
    data = Path(source).read_bytes()
    if data.count(b'\n') + data.count(b'\r') - data.count(b'\r\n') + 1 > 2000000:
        raise PreviewLimit('line budget')
    data, repaired_entries = normalize_empty_dictionary_entries(data)
    dropped = 0
    repaired = Path(destination).with_suffix('.repaired.dxf')
    try:
        if repaired_entries:
            repaired.write_bytes(data)
        parse_source = repaired if repaired_entries else source
        del data
        try:
            doc = ezdxf.readfile(parse_source)
        except ezdxf.DXFStructureError:
            proxy_repaired = Path(destination).with_suffix('.proxy.dxf')
            try:
                dropped = discard_invalid_proxies(parse_source, proxy_repaired)
                doc = ezdxf.readfile(proxy_repaired)
            finally:
                proxy_repaired.unlink(missing_ok=True)
    finally:
        repaired.unlink(missing_ok=True)
    if len(doc.entitydb) > MAX_SOURCE_OBJECTS:
        raise PreviewLimit('source entity budget')
    # A standard build supplies an open CJK font. Glyph outlines remain local
    # and make SVG independent of browser font installations and external URLs.
    cjk = Path('/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc')
    if cjk.is_file():
        fonts.font_manager.scan_folder(cjk.parent)
        fonts.font_manager._fallback_font_name = cjk.name
        for style in doc.styles:
            style.dxf.font = cjk.name
            style.dxf.bigfont = ''
    backend = BoundedSVG()
    frontend = CADFrontend(CADContext(doc), backend)
    frontend.messages += repaired_entries
    if dropped:
        frontend.skipped['ACAD_PROXY_ENTITY'] += dropped
    for drawing_layout in doc.layouts:
        try:
            dict(drawing_layout.get_redraw_order())
        except (ValueError, TypeError):
            # Some converted dictionaries point ACAD_SORTENTS at a dangling
            # handle string instead of a SORTENTSTABLE. Remove only that link.
            drawing_layout.get_extension_dict().dictionary.discard('ACAD_SORTENTS')
            frontend.messages += 1
    model = doc.modelspace()
    entity_count = len(model)
    frontend.draw_layout(model, finalize=True)
    root = backend.get_xml_root_element(layout.Page(0, 0), settings=layout.Settings(max_stroke_width=0.001, min_stroke_width=0.5))
    from ezdxf.math import Vec3
    origin, x_axis, y_axis = backend.transformation_matrix.transform_vertices(
        [Vec3(0, 0, 0), Vec3(1, 0, 0), Vec3(0, 1, 0)])
    matrix = [x_axis.x-origin.x, x_axis.y-origin.y, y_axis.x-origin.x,
              y_axis.y-origin.y, origin.x, origin.y]
    # Dimensions use the normalized SVG coordinate space, never mm CSS units.
    import xml.etree.ElementTree as ET
    box = [float(v) for v in root.attrib['viewBox'].split()]
    if len(box) != 4 or not valid_points([box]) or min(box[2:]) <= 0:
        raise PreviewLimit('invalid viewBox')
    # SVG image intrinsic dimensions are browser pixels, independent of CAD mm.
    # Keep these small while retaining vector detail and the normalized viewBox.
    intrinsic_scale = 1000 / max(box[2:])
    root.set('width', str(box[2] * intrinsic_scale))
    root.set('height', str(box[3] * intrinsic_scale))
    result = dict(svg=ET.tostring(root, encoding='unicode'), width=box[2], height=box[3],
                  modelToSvg=matrix, units=int(doc.units), snapPoints=sorted(frontend.snap_points),
                  entities=entity_count, skipped=sum(frontend.skipped.values()),
                  skippedTypes=dict(frontend.skipped), renderedTypes=dict(frontend.rendered), warnings=frontend.messages,
                  degraded=sum(frontend.degraded.values()), degradedTypes=dict(frontend.degraded),
                  recoveredTypes=dict(frontend.recovered),
                  visits=frontend.visits, points=backend.point_count)
    encoded = json.dumps(result, ensure_ascii=True).encode()
    if len(encoded) >= MAX_BYTES:
        raise PreviewLimit('output budget')
    Path(destination).write_bytes(encoded)


if __name__ == '__main__':
    try:
        render(sys.argv[1], sys.argv[2])
    except PreviewLimit:
        sys.exit(3)
    except Exception:
        # No paths, drawing text or library traces cross the process boundary.
        sys.exit(2)
