"""Patch cell values without reserializing drawings, charts or embedded media."""
from copy import deepcopy
import math
import posixpath
import re
import zipfile

from lxml import etree as ET
from openpyxl.utils.cell import range_boundaries, get_column_letter
from openpyxl.formula.translate import Translator

MAIN = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main'
REL = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships'
PARSER = ET.XMLParser(resolve_entities=False, no_network=True)


def tag(name):
    return '{' + MAIN + '}' + name


def xml(data):
    return ET.fromstring(data, PARSER)


def serialize(tree):
    return ET.tostring(tree, encoding='UTF-8', xml_declaration=True, standalone=True)


def value_element(cell, name):
    element = ET.Element(tag(name))
    extension = cell.find(tag('extLst'))
    cell.insert(cell.index(extension) if extension is not None else len(cell), element)
    return element


def prepare_formula_edit(tree, ref):
    col, row, _, _ = range_boundaries(ref)
    for formula in tree.iter(tag('f')):
        if formula.get('t') == 'array' and formula.get('ref'):
            left, top, right, bottom = range_boundaries(formula.get('ref'))
            if left <= col <= right and top <= row <= bottom:
                raise ValueError('cannot edit an array-formula range')
    cell = next((cell for cell in tree.iter(tag('c')) if cell.get('r') == ref), None)
    formula = cell.find(tag('f')) if cell is not None else None
    if formula is None or formula.get('t') != 'shared':
        return
    group = formula.get('si')
    members = [(cell, cell.find(tag('f'))) for cell in tree.iter(tag('c'))
               if cell.find(tag('f')) is not None and cell.find(tag('f')).get('t') == 'shared'
               and cell.find(tag('f')).get('si') == group]
    anchor = next(((cell, item) for cell, item in members if item.text), None)
    if anchor is None:
        raise ValueError('shared formula has no anchor')
    translator = Translator('=' + anchor[1].text, origin=anchor[0].get('r'))
    for member, item in members:
        item.text = translator.translate_formula(member.get('r'))[1:]
        for key in ('t', 'si', 'ref'):
            item.attrib.pop(key, None)


def relative_part(target):
    value = posixpath.normpath(posixpath.join('xl', target)) if not target.startswith('/') else target[1:]
    if not value.startswith('xl/') or '..' in value.split('/'):
        raise ValueError('invalid workbook relationship')
    return value


def edit_workbook(source, spec, output):
    changes = spec.get('changes', [])
    if not isinstance(changes, list) or not 1 <= len(changes) <= 500:
        raise ValueError('changes must contain 1-500 cells')
    with zipfile.ZipFile(source) as archive:
        workbook = xml(archive.read('xl/workbook.xml'))
        rels = xml(archive.read('xl/_rels/workbook.xml.rels'))
        targets = {item.get('Id'): relative_part(item.get('Target')) for item in rels
                   if item.get('TargetMode') != 'External'}
        sheets = {item.get('name'): targets[item.get('{' + REL + '}id')]
                  for item in workbook.findall(tag('sheets') + '/' + tag('sheet'))}
        styles = xml(archive.read('xl/styles.xml')) if 'xl/styles.xml' in archive.namelist() else None
        formats = styles.find(tag('cellXfs')) if styles is not None else None
        style_variants = {}
        changed_styles = False
        trees = {}
        for change in changes:
            sheet, ref = change.get('sheet'), change.get('ref')
            if sheet not in sheets or not isinstance(ref, str) or not re.fullmatch(r'[A-Z]{1,3}[1-9][0-9]*', ref):
                raise ValueError('invalid sheet or cell reference')
            col, row, _, _ = range_boundaries(ref)
            if col > 16384 or row > 1048576:
                raise ValueError('cell reference out of bounds')
            part = sheets[sheet]
            if not part.startswith('xl/worksheets/'):
                raise ValueError('cannot edit chart sheet')
            if part not in trees:
                tree = xml(archive.read(part))
                trees[part] = tree
            tree = trees[part]
            prepare_formula_edit(tree, ref)
            for merged in tree.findall(tag('mergeCells') + '/' + tag('mergeCell')):
                left, top, right, bottom = range_boundaries(merged.get('ref'))
                if left <= col <= right and top <= row <= bottom and (col, row) != (left, top):
                    raise ValueError('cannot edit merged-cell continuation')
            value = change.get('value')
            if value is not None and not isinstance(value, (str, int, float, bool)):
                raise ValueError('unsupported cell value')
            if isinstance(value, float) and not math.isfinite(value):
                raise ValueError('invalid numeric value')
            if isinstance(value, str) and (len(value) > 32767 or re.search(r'[\x00-\x08\x0b\x0c\x0e-\x1f]', value)):
                raise ValueError('invalid cell text')
            data = tree.find(tag('sheetData'))
            if data is None:
                raise ValueError('worksheet has no cell data element')
            row_element = next((e for e in data if e.get('r') == str(row)), None)
            if row_element is None:
                row_element = ET.Element(tag('row'), r=str(row))
                following = next((e for e in data if int(e.get('r')) > row), None)
                data.insert(data.index(following) if following is not None else len(data), row_element)
            cell = next((e for e in row_element if e.tag == tag('c') and e.get('r') == ref), None)
            if cell is None:
                cell = ET.Element(tag('c'), r=ref)
                following = next((e for e in row_element if e.tag == tag('c') and range_boundaries(e.get('r'))[0] > col), None)
                row_element.insert(row_element.index(following) if following is not None else len(row_element), cell)
            for child in list(cell):
                if child.tag in (tag('f'), tag('v'), tag('is')):
                    cell.remove(child)
            cell.attrib.pop('t', None)
            if isinstance(value, bool):
                cell.set('t', 'b'); value_element(cell, 'v').text = '1' if value else '0'
            elif isinstance(value, (int, float)):
                cell.set('t', 'n'); value_element(cell, 'v').text = str(value)
            elif isinstance(value, str):
                if value.startswith('='):
                    value_element(cell, 'f').text = value[1:]
                else:
                    cell.set('t', 'inlineStr')
                    text = ET.SubElement(value_element(cell, 'is'), tag('t'))
                    text.set('{http://www.w3.org/XML/1998/namespace}space', 'preserve')
                    text.text = value
                if '\n' in value:
                    if formats is None:
                        raise ValueError('workbook styles missing; cannot preserve wrapped cell format')
                    original = int(cell.get('s', '0'))
                    if original not in style_variants:
                        if original >= len(formats):
                            raise ValueError('cell style out of bounds')
                        style = deepcopy(formats[original])
                        alignment = style.find(tag('alignment'))
                        if alignment is None:
                            alignment = ET.Element(tag('alignment'))
                            following = next((e for e in style if e.tag in (tag('protection'), tag('extLst'))), None)
                            style.insert(style.index(following) if following is not None else len(style), alignment)
                        alignment.set('wrapText', '1'); style.set('applyAlignment', '1')
                        style_variants[original] = len(formats)
                        formats.append(style); formats.set('count', str(len(formats)))
                        changed_styles = True
                    cell.set('s', str(style_variants[original]))
            dimension = tree.find(tag('dimension'))
            if dimension is not None:
                left, top, right, bottom = range_boundaries(dimension.get('ref', 'A1'))
                dimension.set('ref', f'{get_column_letter(min(left, col))}{min(top, row)}:{get_column_letter(max(right, col))}{max(bottom, row)}')
        calculation = workbook.find(tag('calcPr'))
        if calculation is None:
            calculation = ET.Element(tag('calcPr'))
            after = {'oleSize', 'customWorkbookViews', 'pivotCaches', 'smartTagPr', 'smartTagTypes', 'webPublishing', 'fileRecoveryPr', 'webPublishObjects', 'extLst'}
            following = next((e for e in workbook if ET.QName(e).localname in after), None)
            workbook.insert(workbook.index(following) if following is not None else len(workbook), calculation)
        calculation.set('fullCalcOnLoad', '1'); calculation.set('forceFullCalc', '1')
        updated = {part: serialize(tree) for part, tree in trees.items()}
        updated['xl/workbook.xml'] = serialize(workbook)
        if changed_styles:
            updated['xl/styles.xml'] = serialize(styles)
        removed = set()
        for relation in list(rels):
            if relation.get('Type', '').endswith('/calcChain'):
                removed.add(relative_part(relation.get('Target')))
                rels.remove(relation)
        if removed:
            updated['xl/_rels/workbook.xml.rels'] = serialize(rels)
            content_types = xml(archive.read('[Content_Types].xml'))
            for item in list(content_types):
                if item.get('PartName', '').lstrip('/') in removed:
                    content_types.remove(item)
            updated['[Content_Types].xml'] = serialize(content_types)
        with zipfile.ZipFile(output, 'w') as result:
            for info in archive.infolist():
                if info.filename not in removed:
                    result.writestr(info, updated.get(info.filename, archive.read(info.filename)))
