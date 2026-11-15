"""Bounded, local-only CAD typeface selection and mixed stroke/outline text."""
import copy
import os
from pathlib import Path

from ezdxf.fonts import fonts
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
        self.glyph_fallbacks, self.missing = set(), set()
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
                face = (get_ttf_font_face(file) if file.suffix.lower() in ('.ttf', '.ttc', '.otf')
                        else get_shape_file_font_face(file))
                self.manager._font_cache.add_entry(file, face)
                count += 1
            except Exception:
                self.profile_errors += 1
        # Previous family lookup misses must not mask newly installed fonts.
        self.manager._match_cache.clear()
        return count

    def family_face(self, family, bold=False, italic=False):
        family = font_key(family)
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
        if requested and self.manager.has_font(requested):
            face, status = self.manager.get_font_face(requested), 'exact-file'
        stem = Path(requested).stem
        if face is None:
            face = self.family_face(family or stem, bold, italic)
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
        key = (requested or str(family), font_key(bigfont), face.filename, status)
        self.records[key] = {'requested': requested or str(family), 'resolved': face.filename,
                             'family': face.family, 'status': status,
                             'bigfont': font_key(bigfont),
                             'bigfontStatus': 'unavailable' if bigfont else 'none'}
        return face, key

    def mark(self, key):
        self.used.add(key)

    def raw(self, name, height, width=1.0):
        key = (name, height, width)
        if key not in self.raw_cache:
            self.raw_cache[key] = self.make_font(name, height, width)
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
        primary = self.raw(name, 1.0)
        candidates = [name, self.cjk.filename, 'wqy-zenhei.ttc',
                      'DejaVuSans.ttf', 'NimbusSans-Regular.otf']
        for candidate in dict.fromkeys(candidates):
            if not self.manager.has_font(candidate):
                continue
            if self.contains(self.raw(candidate, 1.0), char):
                if candidate != name:
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
                'localFontFiles': self.private_count, 'invalidLocalFonts': self.profile_errors}
