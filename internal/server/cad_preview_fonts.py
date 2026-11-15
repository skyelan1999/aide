"""Bounded, local-only CAD typeface selection and mixed stroke/outline text."""
import copy
import os
import struct
from pathlib import Path

from ezdxf.fonts import fonts, shapefile
from ezdxf.fonts.font_measurements import FontMeasurements
from ezdxf.fonts.font_manager import get_ttf_font_face, get_shape_file_font_face
from ezdxf.math import Matrix44
from ezdxf.npshapes import NumpyPath2d


def font_key(value):
    return str(value).replace('\\', '/').rsplit('/', 1)[-1].casefold().strip()


FAMILIES = {
    '宋体': 'simsun', '新宋体': 'nsimsun', 'nsimsun': 'nsimsun',
    '仿宋': 'fangsong', '仿宋_gb2312': 'fangsong', 'fangsong_gb2312': 'fangsong',
    'simfang': 'fangsong', '楷体': 'kaiti', '楷体_gb2312': 'kaiti',
    'kaiti_gb2312': 'kaiti', 'simkai': 'kaiti', '黑体': 'simhei',
    'arialmt': 'arial', 'timesnewromanpsmt': 'times new roman',
}
STROKES = {'txt': 'standard.lff', 'simplex': 'simplex.lff',
           'romans': 'romans.lff', 'isocp': 'iso.lff', 'iso': 'iso.lff'}


class FontBudget(ValueError):
    pass


class BigFont:
    """Read regular indexed Big Fonts, retaining native subshape IDs and advances.

    The container uses little-endian directory integers; character IDs themselves
    are packed code-page bytes in big-endian order. Extended primitive placement
    is deliberately rejected until supported, rather than producing wrong strokes.
    """
    def __init__(self, data, encoding='gbk'):
        if len(data) > 32 << 20 or not data.startswith(b'AutoCAD-86 bigfont 1.0\r\n\x1a'):
            raise ValueError('invalid Big Font header')
        size, count, ranges = struct.unpack_from('<HHH', data, 25)
        end = 31 + ranges * 4 + count * 8
        if size != 8 or not count or ranges > 256 or end > len(data):
            raise ValueError('invalid Big Font directory')
        self.ranges = [struct.unpack_from('<HH', data, 31 + i * 4) for i in range(ranges)]
        if any(not (0 <= a <= b <= 255) for a, b in self.ranges):
            raise ValueError('invalid Big Font lead ranges')
        self.shapes, self.cmap, self.calls = {}, {}, {}
        self.above = self.below = None
        spans, total = [], 0
        for i in range(count):
            code, length, offset = struct.unpack_from('<HHI', data, 31 + ranges * 4 + i * 8)
            if not length:
                continue  # vacant hash-table slots are not character definitions
            if offset < end or offset + length > len(data) or code in self.shapes:
                raise ValueError('invalid Big Font record')
            spans.append((offset, offset + length))
            record = data[offset:offset + length]
            split = record.index(0)
            name, program = record[:split], record[split + 1:]
            if code == 0:
                if len(program) != 4 or program[-1] or program[2] not in (0, 2):
                    raise ValueError('unsupported Big Font metrics')
                self.above, self.below = program[:2]
                self.shapes[0] = shapefile.Symbol(0, length, name, ())
                continue
            codes, calls = self.parse(program)
            total += len(codes)
            if total > 2000000:
                raise FontBudget('Big Font command budget')
            self.shapes[code] = shapefile.Symbol(code, length, name, codes)
            self.calls[code] = calls
            if code > 255 and any(a <= code >> 8 <= b for a, b in self.ranges):
                try:
                    text = code.to_bytes(2, 'big').decode(encoding, errors='strict')
                    if len(text) == 1:
                        self.cmap[ord(text)] = code
                except UnicodeError:
                    pass
        spans.sort()
        if any(b > c for (_, b), (c, _) in zip(spans, spans[1:])):
            raise ValueError('overlapping Big Font records')
        if self.above is None or not self.cmap:
            raise ValueError('missing Big Font metrics or characters')
        self.shapes.pop(0, None)
        # Memoized DAG validation also bounds expanded command work: a shallow
        # graph alone does not prevent exponentially repeated subshape calls.
        costs = {}
        def visit(code, chain):
            if code in chain or len(chain) >= 32 or code not in self.shapes:
                raise ValueError('invalid Big Font subshape graph')
            if code in costs:
                return costs[code]
            cost = len(self.shapes[code].data)
            depth = 1
            for child in self.calls.get(code, ()):
                child_cost, child_depth = visit(child, chain + (code,))
                cost += child_cost
                depth = max(depth, child_depth + 1)
            if cost > 100000 or depth > 32:
                raise FontBudget('Big Font expansion budget')
            costs[code] = (cost, depth)
            return costs[code]
        for code in self.calls:
            visit(code, ())

    def parse(self, program):
        reader = shapefile.DataReader(program)
        codes, calls = [], []
        while reader.has_data:
            code = reader.u8(); codes.append(code)
            if code == 0:
                # Opcode 0 ends the program. Indexed legacy records may retain
                # allocation padding after it, including stale nonzero bytes.
                # Never interpret that tail; directory spans and raw-byte limits
                # still bound the complete record and all referenced programs.
                return tuple(codes), calls
            if code in (3, 4):
                value = reader.u8()
                if not value:
                    raise ValueError('zero Big Font scale')
                codes.append(value)
            elif code == 7:
                child = reader.u8()
                if not child:
                    raise ValueError('extended Big Font primitives unsupported')
                if any(a <= child <= b for a, b in self.ranges):
                    child = child * 256 + reader.u8()
                codes.append(child); calls.append(child)
            elif code in (8, 12):
                codes.extend(reader.i8() for _ in range(3 if code == 12 else 2))
            elif code in (9, 13):
                while True:
                    x, y = reader.i8(), reader.i8(); codes.extend((x, y))
                    if not (x or y):
                        break
                    if code == 13:
                        codes.append(reader.i8())
            elif code == 10:
                codes.extend((reader.u8(), reader.octant()))
            elif code == 11:
                codes.extend(reader.u8() for _ in range(4)); codes.append(reader.octant())
            elif code == 15:
                raise ValueError('unsupported Big Font opcode')
            if reader.index > len(program) or len(codes) > 16384:
                raise FontBudget('Big Font glyph budget')
        raise ValueError('unterminated Big Font program')

    def cache(self, inherited_height):
        shape = shapefile.ShapeFile(b'Big Font', self.above or inherited_height,
                                    self.below if self.above else 0)
        shape.shapes = self.shapes
        cmap = self.cmap

        class Cache(shapefile.GlyphCache):
            def detect_space_width(self, default_width):
                # Low IDs belong to primitives, not ASCII space/A/x/p glyphs.
                return default_width

            def get_empty_box(self, char=65):
                from ezdxf import path
                from ezdxf.npshapes import NumpyPath2d
                p = path.Path((0, 0))
                p.line_to((shape.above, 0)); p.line_to((shape.above, shape.above))
                p.line_to((0, shape.above)); p.close()
                p.move_to((shape.above, 0))
                return NumpyPath2d(p)

            def _render_shape(self, number):
                if number not in cmap:
                    raise shapefile.UnsupportedShapeNumber(number)
                return super()._render_shape(cmap[number])

            def _get_font_measurements(self):
                return FontMeasurements(0, shape.above, shape.above * 2 / 3, shape.below)

        cache = Cache(shape)
        cache.cmap = cmap
        return cache


class MixedFont(fonts.AbstractFont):
    """Measure and position the same font runs that the pipeline actually draws."""
    def __init__(self, primary, owner):
        super().__init__(primary.measurements)
        self.primary, self.owner = primary, owner
        self.name = primary.name
        self.cap_height, self.width_factor = primary.cap_height, primary.width_factor
        self.font_render_type = primary.font_render_type

    def runs(self, text):
        name, chunk = None, ''
        for char in text:
            selected = self.owner.glyph_font(self.name, char)
            if name is not None and selected != name:
                yield name, chunk
                chunk = ''
            name, chunk = selected, chunk + char
        if chunk:
            yield name, chunk

    def text_width_ex(self, text, cap_height, width_factor=1.0):
        return sum(self.owner.raw(name, cap_height, width_factor).text_width(part)
                   for name, part in self.runs(text))

    def text_width(self, text):
        return self.text_width_ex(text, self.cap_height, self.width_factor)

    def space_width(self):
        return self.primary.space_width()

    def text_glyph_paths(self, text, cap_height, width_factor=1.0):
        result, offset = [], 0.0
        for name, part in self.runs(text):
            font = self.owner.raw(name, cap_height, width_factor)
            paths = font.text_glyph_paths(part, cap_height, width_factor)
            matrix = Matrix44.translate(offset, 0, 0)
            for path in paths:
                path.transform_inplace(matrix)
            result.extend(paths)
            offset += font.text_width(part)
        return result

    def text_path_ex(self, text, cap_height, width_factor=1.0):
        paths = self.text_glyph_paths(text, cap_height, width_factor)
        return NumpyPath2d.concatenate(paths) if paths else NumpyPath2d(None)

    def text_path(self, text):
        return self.text_path_ex(text, self.cap_height, self.width_factor)


class CADFonts:
    def __init__(self):
        self.make_font = fonts.make_font
        self.find_name = fonts.find_font_file_name
        self.raw_cache, self.mixed_cache, self.choice_cache = {}, {}, {}
        self.records, self.used = {}, set()
        self.bigfonts, self.loaded_bigfonts, self.pairs, self.pair_faces = {}, {}, {}, {}
        self.invalid_shapes = set()
        self.glyph_fallbacks, self.missing, self.bigfont_glyphs = set(), set(), set()
        self.profile_errors = 0
        self.manager = fonts.font_manager
        self.saved_state = {name: getattr(self.manager, name) for name in
                            ('_font_cache', '_match_cache', '_loaded_ttf_fonts',
                             '_loaded_shape_file_glyph_caches', '_loaded_lff_glyph_caches',
                             '_fallback_font_name')}
        self.saved_glyphs = fonts._CachedFont._glyph_caches
        self.manager._font_cache = copy.deepcopy(self.manager._font_cache)
        for name in ('_match_cache', '_loaded_ttf_fonts', '_loaded_shape_file_glyph_caches',
                     '_loaded_lff_glyph_caches'):
            setattr(self.manager, name, {})
        fonts._CachedFont._glyph_caches = {}
        try:
            self.private_count = self.scan(Path(os.environ.get('AIDE_CAD_FONT_DIR', '/data/cad-fonts')))
            self.scan(Path('/opt/aide/cad-fonts'))
            self.cjk = self.family_face('fangsong') or self.family_face('simsun')
            if self.cjk is None:
                self.manager.scan_folder(Path('/usr/share/fonts/truetype/wqy'))
                self.cjk = self.manager.get_font_face('wqy-zenhei.ttc')
        except Exception:
            self.__exit__()
            raise

    def scan(self, directory):
        if not directory.is_dir():
            return 0
        files = sorted(p for p in directory.iterdir() if p.suffix.lower() in
                       ('.ttf', '.ttc', '.otf', '.shx', '.shp', '.lff'))
        if len(files) > 32:
            raise FontBudget('font file budget')
        total, count = 0, 0
        for file in files:
            if file.is_symlink() or not file.is_file():
                self.profile_errors += 1
                continue
            size = file.stat().st_size
            total += size
            if size > 32 << 20 or total > 128 << 20:
                raise FontBudget('font byte budget')
            try:
                with file.open('rb') as stream:
                    big_header = file.suffix.lower() == '.shx' and stream.read(25).startswith(b'AutoCAD-86 bigfont')
                if big_header:
                    self.bigfonts[font_key(file.name)] = file
                    count += 1
                    continue
                face = (get_ttf_font_face(file) if file.suffix.lower() in ('.ttf', '.ttc', '.otf')
                        else get_shape_file_font_face(file))
                self.manager._font_cache.add_entry(file, face)
                count += 1
            except Exception:
                self.profile_errors += 1
                self.invalid_shapes.add(font_key(file.name))
        # Previous family lookup misses must not mask newly installed fonts.
        self.manager._match_cache.clear()
        return count

    def load_bigfont(self, name):
        # The profile remains byte/count bounded at scan time. Parse only fonts
        # referenced by this drawing, so unrelated large dictionaries do not
        # consume the worker's fixed address-space budget.
        if name not in self.loaded_bigfonts:
            try:
                self.loaded_bigfonts[name] = BigFont(self.bigfonts[name].read_bytes())
            except MemoryError:
                raise FontBudget('font memory budget')
            except (ValueError, IndexError, struct.error):
                self.profile_errors += 1
                self.invalid_shapes.add(name)
                self.loaded_bigfonts[name] = None
        return self.loaded_bigfonts[name]

    def family_face(self, family, bold=False, italic=False):
        family = font_key(family)
        if not family:
            return None
        family = FAMILIES.get(family, family)
        style = 'Bold Italic' if bold and italic else 'Bold' if bold else 'Italic' if italic else 'Regular'
        return self.manager.find_best_match(family=family, style=style,
                                            weight=700 if bold else 400, italic=italic)

    def resolve(self, requested, extended=('', False, False), bigfont=''):
        parts = font_key(requested).split(',', 1)
        requested = parts[0]
        if len(parts) > 1 and not bigfont:
            bigfont = parts[1]
        family, italic, bold = extended
        face, status = None, 'fallback'
        lookup = requested
        if requested and not Path(requested).suffix and self.manager.has_font(requested + '.shx'):
            lookup += '.shx'
        if lookup and self.manager.has_font(lookup):
            face, status = self.manager.get_font_face(lookup), 'exact-file'
        stem = Path(requested).stem
        if face is None:
            face = self.family_face(stem, bold, italic) or self.family_face(family, bold, italic)
            if face is not None:
                status = ('stroke-substitute' if (Path(requested).suffix in ('.shx', '.shp')
                          or stem in STROKES) else 'same-family')
        if face is None and stem in STROKES:
            mapped = STROKES[stem]
            if self.manager.has_font(mapped):
                face, status = self.manager.get_font_face(mapped), 'stroke-substitute'
        if face is None:
            face = self.cjk
        # Unsupported shape encodings must not be reported as an exact font.
        try:
            actual = self.raw(face.filename, 1.0)
            if (Path(face.filename).suffix.lower() in ('.shx', '.shp')
                    and actual.font_render_type != fonts.FontRenderType.STROKE):
                face, status = self.cjk, 'unsupported-shape-substitute'
        except (ValueError, RuntimeError, fonts.UnsupportedFont):
            face, status = self.cjk, 'unsupported-font-substitute'
        resolved = face.filename
        big_key = font_key(bigfont)
        if big_key and not Path(big_key).suffix:
            big_key += '.shx'
        big_status = ('unsupported-font' if big_key in self.invalid_shapes else 'unavailable') if bigfont else 'none'
        if big_key in self.bigfonts and self.raw(face.filename, 1.0).font_render_type != fonts.FontRenderType.STROKE:
            big_status = 'requires-stroke-primary'
        elif big_key in self.bigfonts and self.load_bigfont(big_key) is not None:
            pair_key = (face.filename, big_key)
            if pair_key not in self.pair_faces:
                if len(self.pair_faces) >= 128:
                    raise FontBudget('Big Font pairing budget')
                primary = self.raw(face.filename, 1.0)
                inherited = primary.glyph_cache.font_measurements.cap_height
                pair_name = f'aide-pair-{len(self.pairs)}.shx'
                secondary = f'aide-big-{len(self.pairs)}.shx'
                secondary_face = face._replace(filename=secondary, family=big_key)
                self.manager._font_cache.add_entry(Path(secondary), secondary_face)
                self.manager._loaded_shape_file_glyph_caches[secondary] = self.loaded_bigfonts[big_key].cache(inherited)
                self.pairs[pair_name] = (face.filename, secondary)
                pair_face = face._replace(filename=pair_name)
                self.manager._font_cache.add_entry(Path(pair_name), pair_face)
                self.pair_faces[pair_key] = pair_face
            face = self.pair_faces[pair_key]
            big_status = 'exact-file'
        elif big_key in self.invalid_shapes:
            big_status = 'unsupported-font'

        key = (requested or str(family), font_key(bigfont), resolved, status)
        self.records[key] = {'requested': requested or str(family), 'resolved': resolved,
                             'family': face.family, 'status': status,
                             'bigfont': font_key(bigfont),
                             'bigfontStatus': big_status}
        return face, key

    def mark(self, key):
        self.used.add(key)

    def raw(self, name, height, width=1.0):
        key = (name, height, width)
        if key not in self.raw_cache:
            self.raw_cache[key] = self.make_font(self.pairs.get(name, (name,))[0], height, width)
        return self.raw_cache[key]

    @staticmethod
    def contains(font, char):
        if ord(char) <= 32:
            return True
        cache = font.glyph_cache
        if hasattr(cache, 'cmap'):
            return ord(char) in cache.cmap
        try:
            cache.font[ord(char)]
            return True
        except (KeyError, TypeError, AttributeError):
            return False

    def glyph_font(self, name, char):
        key = (name, char)
        if key in self.choice_cache:
            return self.choice_cache[key]
        primary_name = self.pairs.get(name, (name,))[0]
        secondary = self.pairs.get(name, (None, None))[1]
        candidates = ([secondary] if secondary and ord(char) > 127 else []) + [primary_name, self.cjk.filename, 'wqy-zenhei.ttc',
                      'DejaVuSans.ttf', 'NimbusSans-Regular.otf']
        for candidate in dict.fromkeys(candidates):
            if not self.manager.has_font(candidate):
                continue
            if self.contains(self.raw(candidate, 1.0), char):
                if candidate == secondary:
                    self.bigfont_glyphs.add((secondary, ord(char)))
                if candidate not in (primary_name, secondary):
                    self.glyph_fallbacks.add((name, ord(char), candidate))
                self.choice_cache[key] = candidate
                return candidate
        self.missing.add((name, ord(char)))
        self.choice_cache[key] = name
        return name

    def mixed(self, name, height, width=1.0):
        key = (name, height, width)
        if key not in self.mixed_cache:
            self.mixed_cache[key] = MixedFont(self.raw(name, height, width), self)
            self.mixed_cache[key].name = name
        return self.mixed_cache[key]

    def file_name(self, face):
        if face.filename and self.manager.has_font(font_key(face.filename)):
            return self.manager.get_font_face(font_key(face.filename)).filename
        resolved, key = self.resolve(face.family, (face.family, face.is_italic, face.is_bold))
        self.mark(key)
        return resolved.filename

    def __enter__(self):
        fonts.make_font = self.mixed
        fonts.find_font_file_name = self.file_name
        return self

    def __exit__(self, *args):
        fonts.make_font = self.make_font
        fonts.find_font_file_name = self.find_name
        for name, value in self.saved_state.items():
            setattr(self.manager, name, value)
        fonts._CachedFont._glyph_caches = self.saved_glyphs

    def bind_pipeline(self, pipeline):
        from ezdxf.addons.drawing.pipeline import prepare_string_for_rendering
        draw = pipeline.draw_text

        def draw_runs(text, transform, properties, cap_height, dxftype='TEXT'):
            text = prepare_string_for_rendering(text, dxftype)
            face = properties.font or pipeline.default_font_face
            face = self.manager.get_font_face(self.file_name(face))
            properties = copy.copy(properties)
            properties.font = face
            runs = list(self.mixed(face.filename, 1.0).runs(text))
            if len(runs) <= 1 and (not runs or runs[0][0] == face.filename):
                return draw(text, transform, properties, cap_height, dxftype)
            offset = 0.0
            for name, part in runs:
                props = copy.copy(properties)
                props.font = self.manager.get_font_face(name)
                matrix = Matrix44.translate(offset, 0, 0) @ transform
                draw(part, matrix, props, cap_height, dxftype)
                offset += self.raw(name, cap_height).text_width(part)
        pipeline.draw_text = draw_runs

    def report(self):
        rows = [self.records[k] for k in sorted(self.used)]
        return {'faces': rows, 'originalFaces': sum(r['status'] in ('exact-file', 'same-family') for r in rows),
                'substitutedFaces': sum(r['status'] not in ('exact-file', 'same-family') for r in rows),
                'glyphFallbacks': len(self.glyph_fallbacks), 'missingGlyphs': len(self.missing),
                'missingCodepoints': sorted({f'U+{code:04X}' for _,code in self.missing}),
                'bigfontGlyphs': len(self.bigfont_glyphs),
                'originalBigFonts': sum(r['bigfontStatus'] == 'exact-file' for r in rows),
                'substitutedBigFonts': sum(r['bigfontStatus'] not in ('none', 'exact-file') for r in rows),
                'localFontFiles': self.private_count, 'invalidLocalFonts': self.profile_errors}
