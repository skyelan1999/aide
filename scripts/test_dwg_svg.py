#!/usr/bin/env python3
"""Real ezdxf rendering regression; run inside the standard CAD runtime."""
import importlib.util
import json
import math
import re
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import xml.etree.ElementTree as ET

import ezdxf

spec = importlib.util.spec_from_file_location('dwg_svg', Path(__file__).resolve().parents[1] / 'internal/server/dwg_preview_svg.py')
cad = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cad)


class CADSVGTests(unittest.TestCase):
    def render(self, doc):
        with tempfile.TemporaryDirectory() as td:
            source, output = Path(td)/'input.dxf', Path(td)/'output.json'
            doc.saveas(source)
            cad.render(source, output)
            return json.loads(output.read_text())

    def test_cad_entities_attributes_text_dimensions_and_nurbs(self):
        self.assertTrue(Path('/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc').is_file())
        doc = ezdxf.new('R2018'); model = doc.modelspace()
        model.add_line((0, 0), (10, 0))
        model.add_hatch(color=1).paths.add_polyline_path([(0, 0), (4, 0), (4, 3), (0, 3)], is_closed=True)
        model.add_leader([(0, 0, 0), (3, 5, 0)], dxfattribs={'dimstyle': 'Standard'})
        model.add_mtext(r'{\C1;锅炉}\P试验', dxfattribs={'char_height': 0.5})
        curve = model.add_rational_spline([(1, 0), (1, 1), (0, 1)], [1, math.sqrt(0.5), 1], degree=2)
        block = doc.blocks.new('valve', base_point=(10, 20))
        block.add_line((10, 20), (11, 20))
        block.add_attdef('ID', (10, 20), text='DEFAULT', height=0.5)
        model.add_blockref('valve', (20, 30), dxfattribs={'rotation': 30, 'xscale': 2, 'yscale': 2}).add_auto_attribs({'ID': 'V-101'})
        dimension = model.add_linear_dim(base=(0, 8), p1=(0, 0), p2=(10, 0)); dimension.render()
        result = self.render(doc)
        self.assertEqual(result['skipped'], 0)
        for kind in ('HATCH', 'LEADER', 'ATTRIB', 'MTEXT', 'DIMENSION', 'SPLINE'):
            self.assertGreater(result['renderedTypes'].get(kind, 0), 0, kind)
        root = ET.fromstring(result['svg'])
        paths = [e.attrib.get('d', '') for e in root.iter() if e.tag.endswith('path')]
        self.assertTrue(any('C' in path or 'c' in path for path in paths), 'curve must use evaluated Beziers, not a control polygon')
        for element in root.iter():
            self.assertNotIn(element.tag.rsplit('}', 1)[-1], ('script', 'foreignObject', 'image'))
            for name, value in element.attrib.items():
                if name.endswith('href'):
                    self.assertTrue(value.startswith('#'))

    def test_model_units_do_not_make_strokes_invisible(self):
        doc = ezdxf.new(); model = doc.modelspace()
        model.add_line((0, 0), (50000, 10000), dxfattribs={"lineweight": 0})
        result = self.render(doc)
        widths = [float(value) for value in re.findall(r'stroke-width: ([0-9.]+)', result['svg'])]
        self.assertTrue(widths)
        self.assertTrue(0.18 < result['height'] / result['width'] < 0.22, 'fit must keep the drawing aspect ratio, without an oversized blank page')
        self.assertGreater(min(widths) / result['width'] * 1000, 0.2, 'stroke must remain readable on a 1000px-wide preview')
        root = ET.fromstring(result['svg'])
        self.assertLessEqual(max(float(root.attrib[k]) for k in ('width','height')), 1000)

    def test_transparent_records_do_not_inflate_fit_bounds(self):
        from ezdxf.addons.drawing.backend import BackendProperties
        from ezdxf.math import Vec2
        backend = cad.BoundedSVG()
        backend.draw_line(Vec2(0,0), Vec2(100,50), BackendProperties(color='#000000'))
        count = len(backend.records)
        from ezdxf.npshapes import NumpyPoints2d
        backend.draw_filled_polygon(NumpyPoints2d([Vec2(-1e11,-1e11),Vec2(1e11,-1e11),Vec2(1e11,1e11)]), BackendProperties(color='#ffffff00'))
        self.assertEqual(len(backend.records), count)
        self.assertEqual(backend.point_count, 2)

    def test_model_transform_units_and_world_snaps(self):
        doc = ezdxf.new(); doc.units = 4
        doc.modelspace().add_line((100, 200), (1100, 700))
        block = doc.blocks.new('snap')
        block.add_line((0, 0), (10, 0))
        doc.modelspace().add_blockref('snap', (300, 400), dxfattribs={'rotation': 90, 'xscale': 2, 'yscale': 2})
        result = self.render(doc)
        self.assertEqual(result['units'], 4)
        for p in ([100,200], [1100,700], [300,400], [300,420]):
            self.assertTrue(any(math.dist(p,q)<1e-7 for q in result['snapPoints']), p)
        a,b,c,d,e,f = result['modelToSvg']
        self.assertAlmostEqual(a, -d, places=5)
        self.assertEqual(b, 0); self.assertEqual(c, 0)
        x,y = a*100+c*200+e, b*100+d*200+f
        root = ET.fromstring(result['svg'])
        paths = [p.attrib.get('d','') for p in root.iter() if p.tag.endswith('path')]
        starts = [re.match(r'M ?(-?[0-9.]+)[ ,]+(-?[0-9.]+)',p) for p in paths]
        self.assertTrue(any(m and abs(float(m[1])-x)<2 and abs(float(m[2])-y)<2 for m in starts))
        backgrounds = [p for p in root if p.tag.endswith('rect')]
        self.assertTrue(all(float(p.attrib.get('fill-opacity',1))==0 for p in backgrounds))

    def test_missing_and_astronomical_splines_are_reported(self):
        doc = ezdxf.new(); model = doc.modelspace()
        model.add_line((0, 0), (5, 5))
        model.add_spline()
        model.add_spline([(0, 0), (1e278, 1), (3, 3)])
        result = self.render(doc)
        self.assertEqual(result['skippedTypes'], {'SPLINE': 2})
        self.assertEqual(result['renderedTypes']['LINE'], 1)
        self.assertTrue(0 < result['width'] <= 1e8 and 0 < result['height'] <= 1e8)

    def test_invalid_hatch_scale_recovers_boundaries_without_fabricating_fill(self):
        doc = ezdxf.new(); model = doc.modelspace()
        model.add_line((0, 0), (10, 10))
        hatch = model.add_hatch()
        hatch.paths.add_polyline_path([(1, 1), (9, 1), (9, 9), (1, 9)], is_closed=True)
        hatch.set_pattern_fill('ANSI31', scale=1)
        hatch.dxf.__dict__['pattern_scale'] = 0.0
        result = self.render(doc)
        self.assertEqual(result['degradedTypes'], {'HATCH': 1})
        self.assertEqual(result['renderedTypes']['HATCH'], 1)
        self.assertEqual(result['renderedTypes']['LINE'], 1)
        self.assertEqual(result['skipped'], 0)

    def test_null_dictionary_entries_preserve_named_references_and_geometry(self):
        source = b'0\nDICTIONARY\n5\nC\n3\n\n350\n0\n3\nACAD_GROUP\n350\nD\n3\nACAD_LAYOUT\n350\nAA\n0\nLINE\n3\n\n350\n0\n'
        normalized, count = cad.normalize_empty_dictionary_entries(source)
        self.assertEqual(count, 1)
        self.assertEqual(normalized, source.replace(b'3\n\n350\n0\n', b'', 1))
        self.assertEqual(cad.normalize_empty_dictionary_entries(normalized), (normalized, 0))

    def test_cycles_geometry_and_text_stay_bounded(self):
        doc = ezdxf.new(); block = doc.blocks.new('loop')
        block.add_blockref('loop', (0, 0)); doc.modelspace().add_blockref('loop', (0, 0))
        with self.assertRaises(cad.PreviewLimit): self.render(doc)
        doc = ezdxf.new(); doc.modelspace().add_mtext('x' * 16385)
        with self.assertRaises(cad.PreviewLimit): self.render(doc)
        backend = cad.BoundedSVG()
        from ezdxf.addons.drawing.backend import BackendProperties
        from ezdxf.math import Vec2
        backend.point_count = cad.MAX_POINTS
        with self.assertRaises(cad.PreviewLimit): backend.draw_line(Vec2(0, 0), Vec2(1, 1), BackendProperties())

    def test_invalid_proxy_is_dropped_as_a_record_without_repairing_geometry(self):
        with tempfile.TemporaryDirectory() as td:
            source, target = Path(td)/'input.dxf', Path(td)/'fixed.dxf'
            before = b'0\nLINE\n10\n12.5\n'
            after = b'0\nLINE\n10\n99.25\n0\nEOF\n'
            source.write_bytes(before+b'0\nACAD_PROXY_ENTITY\n310\nnot-hex\n'+after)
            original = source.read_bytes()
            self.assertEqual(cad.discard_invalid_proxies(source, target), 1)
            self.assertEqual(target.read_bytes(), before+after)
            self.assertEqual(source.read_bytes(), original)
            source.write_bytes(before+b'0\nXRECORD\n310\nnot-hex\n'+after)
            with self.assertRaises(ValueError): cad.discard_invalid_proxies(source, target)

    def test_invalid_redraw_dictionary_does_not_hide_model_geometry(self):
        doc = ezdxf.new(); model = doc.modelspace()
        model.add_line((0,0),(100,50))
        xdict = model.block_record.new_extension_dict()
        xdict.dictionary['ACAD_SORTENTS'] = '0'
        result = self.render(doc)
        self.assertEqual(result['renderedTypes']['LINE'], 1)
        self.assertEqual(result['warnings'], 1)

    def test_tiny_metadata_text_keeps_original_height(self):
        doc = ezdxf.new()
        doc.modelspace().add_line((0, 0), (10, 10))
        text = doc.modelspace().add_text('metadata', dxfattribs={'height': 1e-29})
        result = self.render(doc)
        self.assertEqual(text.dxf.height, 1e-29)
        self.assertEqual(result['skipped'], 0)
        self.assertEqual(result['renderedTypes']['TEXT'], 1)

    def test_fixed_single_column_font_metrics_do_not_drop_label(self):
        from ezdxf.entities.mtext import MTextColumns
        doc = ezdxf.new('R2018')
        doc.modelspace().add_line((0, 0), (10, 10))
        text = doc.modelspace().add_mtext(r'{\C1;Readable label}', dxfattribs={'char_height': 1, 'width': 20})
        text.setup_columns(MTextColumns.new_static_columns(1, 20, 0, 0.1), linked=False)
        result = self.render(doc)
        self.assertEqual(result['skipped'], 0)
        self.assertEqual(result['recoveredTypes'].get('MTEXT_AUTO_HEIGHT'), 1)
        self.assertEqual(result['renderedTypes']['MTEXT'], 1)
        self.assertEqual(text.columns.defined_height, 0.1)

    def test_zero_width_formatting_only_mtext_has_no_missing_glyphs(self):
        doc = ezdxf.new('R2018')
        doc.modelspace().add_line((0, 0), (10, 10))
        doc.modelspace().add_mtext(r'{\C1;}', dxfattribs={'width': 0, 'char_height': 1})
        result = self.render(doc)
        self.assertEqual(result['skipped'], 0)
        self.assertEqual(result['recoveredTypes'].get('MTEXT_EMPTY'), 1)

    def test_acis_topology_cycles_and_open_loops_reject(self):
        from types import SimpleNamespace
        nil = SimpleNamespace(is_none=True)
        a = SimpleNamespace(is_none=False, next=nil)
        with self.assertRaises(ValueError): list(cad.acis_chain(a, 'next', circular=True))
        b = SimpleNamespace(is_none=False, next=a)
        a.next = b
        with self.assertRaises(ValueError): list(cad.acis_chain(a, 'next'))
        self.assertEqual(len(list(cad.acis_chain(a, 'next', circular=True))), 2)

    def test_sab_region_preserves_curves_and_block_transform(self):
        from ezdxf.acis import api, entities
        from ezdxf.math import Vec3, Matrix44
        from ezdxf.render import MeshBuilder
        mesh = MeshBuilder(); mesh.add_face([(5, 0, 0), (0, 5, 0), (-5, 0, 0), (0, -5, 0)])
        body = api.body_from_mesh(mesh)
        face = body.lump.shell.face
        surface = cad.PreviewPlaneSurface()
        surface.origin, surface.normal, surface.u_axis = Vec3(), Vec3(0,0,1), Vec3(1,0,0)
        surface.reverse_v = False
        face.surface = surface
        for coedge in face.loop.coedges():
            edge = coedge.edge
            curve = cad.PreviewEllipseCurve()
            curve.center, curve.normal, curve.major, curve.ratio = Vec3(), Vec3(0,0,1), Vec3(5,0,0), 1.0
            edge.curve = curve
            a, b = edge.start_vertex.point.location, edge.end_vertex.point.location
            edge.start_param = math.atan2(a.y, a.x)
            edge.end_param = math.atan2(b.y, b.x)
            while edge.end_param <= edge.start_param: edge.end_param += math.tau
        # A synthetic SAB fixture, assembled independently through ezdxf's
        # public ACIS exporter; no customer B-rep or drawing enters the repo.
        def plane_data(self, writer):
            writer.write_loc_vec3(self.origin); writer.write_dir_vec3(self.normal)
            writer.write_dir_vec3(self.u_axis); writer.write_bool(self.reverse_v, 'reverse_v', 'forward_v')
            entities.Surface.write_data(self, writer)
        def ellipse_data(self, writer):
            writer.write_loc_vec3(self.center); writer.write_dir_vec3(self.normal)
            writer.write_dir_vec3(self.major); writer.write_double(self.ratio)
            entities.Curve.write_data(self, writer)
        with patch.object(cad.PreviewPlaneSurface, 'write_data', plane_data), patch.object(cad.PreviewEllipseCurve, 'write_data', ellipse_data):
            sab = api.export_sab([body])
        doc = ezdxf.new('R2018'); region = doc.modelspace().add_region(); region.sab = sab
        paths = cad.region_paths(region)
        self.assertEqual(len(paths), 1)
        points = list(paths[0].flattening(0.001))
        self.assertTrue(all(abs(Vec3(p).magnitude-5) < 0.003 for p in points))
        self.assertLess(paths[0].start.distance(paths[0].end), 1e-8)
        region.transform(Matrix44.translate(100, 200, 0))
        moved = cad.region_paths(region)[0]
        self.assertTrue(moved.start.isclose(paths[0].start+Vec3(100,200,0)))
        result = self.render(doc)
        self.assertEqual(result['skipped'], 0)
        self.assertGreaterEqual(result['renderedTypes']['REGION'], 1)


if __name__ == '__main__':
    unittest.main()
