"""Bounded, addressable Office text for AI. Patch only selected XML members.

Text edits preserve paragraph properties and opaque media/chart parts. Runs in
an edited paragraph retain their properties, with replacement text in its first
text run. Complex field/object/track-change paragraphs are readable, not editable.
"""
import json
import re
import sys
import zipfile
from lxml import etree as ET
from xlsx_patch import edit_workbook, relative_part

PARSER = ET.XMLParser(resolve_entities=False, no_network=True)
W = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'
A = 'http://schemas.openxmlformats.org/drawingml/2006/main'
S = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main'
R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships'
XMLSPACE = '{http://www.w3.org/XML/1998/namespace}space'


def parse(data):
    if b'<!DOCTYPE' in data or b'<!ENTITY' in data:
        raise ValueError('XML declarations are not supported')
    return ET.fromstring(data, PARSER)


def blocks(archive, kind):
    if kind == 'xlsx':
        shared = []
        if 'xl/sharedStrings.xml' in archive.namelist():
            for item in parse(archive.read('xl/sharedStrings.xml')):
                shared.append(''.join(item.itertext()))
        rels = {item.get('Id'): relative_part(item.get('Target'))
                for item in parse(archive.read('xl/_rels/workbook.xml.rels'))}
        workbook = parse(archive.read('xl/workbook.xml'))
        for sheet in workbook.findall('.//{%s}sheet' % S):
            part = rels[sheet.get('{%s}id' % R)]
            for cell in parse(archive.read(part)).iter('{%s}c' % S):
                ref = cell.get('r')
                formula = cell.find('{%s}f' % S)
                value = cell.find('{%s}v' % S)
                text = value.text or '' if value is not None else ''
                if formula is not None:
                    text = '=' + (formula.text or '')
                elif cell.get('t') == 's':
                    text = shared[int(text)]
                elif cell.get('t') == 'inlineStr':
                    text = ''.join(cell.itertext())
                yield {'id': sheet.get('name') + '!' + ref, 'sheet': sheet.get('name'),
                       'cell': ref, 'text': text, 'editable': True}, part, cell
        return
    ns = W if kind == 'docx' else A
    parts = ['word/document.xml'] if kind == 'docx' else sorted(
        (p for p in archive.namelist() if re.fullmatch(r'ppt/slides/slide\d+\.xml', p)),
        key=lambda p: int(re.search(r'(\d+)\.xml', p).group(1)))
    for part in parts:
        tree = parse(archive.read(part))
        for index, paragraph in enumerate(tree.iter('{%s}p' % ns)):
            texts = list(paragraph.iter('{%s}t' % ns))
            text = ''.join(t.text or '' for t in texts)
            # Do not rewrite fields, tabs, drawings, hyperlinks or revision text.
            allowed = {'p', 'pPr', 'r', 'rPr', 't', 'endParaRPr', 'bookmarkStart', 'bookmarkEnd',
                       'commentRangeStart', 'commentRangeEnd', 'commentReference', 'proofErr'}
            def is_complex(node):
                # Formatting subtrees can contain arbitrarily nested font,
                # bullet and language properties after an editor roundtrip.
                parent = node.getparent()
                while parent is not None and parent is not paragraph:
                    if ET.QName(parent).localname in ('pPr', 'rPr', 'endParaRPr'):
                        return False
                    parent = parent.getparent()
                return ET.QName(node).localname not in allowed
            complex_content = any(is_complex(node) for node in paragraph.iter())
            yield {'id': part + '#' + str(index), 'text': text,
                   'editable': bool(texts) and not complex_content,
                   'part': part, 'paragraph': index}, tree, paragraph


def execute(operation, source, kind, spec, output):
    if kind not in ('docx', 'xlsx', 'pptx'):
        raise ValueError('AI structure/edit supports docx, xlsx, pptx; convert legacy/ODF using the editor first')
    with zipfile.ZipFile(source) as archive:
        if operation == 'read':
            offset = spec.get('offset', 0)
            if not isinstance(offset, int) or offset < 0 or offset > 100000:
                raise ValueError('invalid offset')
            result = []
            total_chars = 0
            more = False
            for number, (block, _, _) in enumerate(blocks(archive, kind)):
                if number < offset:
                    continue
                if len(block['text']) > 60000 and not result:
                    block['text'] = block['text'][:60000]
                    block['editable'] = False
                    block['textTruncated'] = True
                if len(result) >= 200 or total_chars + len(block['text']) > 60000:
                    more = True
                    break
                result.append(block)
                total_chars += len(block['text'])
            with open(output, 'w', encoding='utf-8') as f:
                json.dump({'format': kind, 'blocks': result, 'offset': offset,
                           'nextOffset': offset + len(result) if more else None,
                           'truncated': more}, f, ensure_ascii=False)
            return
        changes = spec.get('changes')
        if not isinstance(changes, list) or not 1 <= len(changes) <= 100:
            raise ValueError('expected 1..100 changes')
        wanted = {}
        for change in changes:
            key = change.get('id')
            if not isinstance(key, str) or key in wanted or not isinstance(change.get('expectedText'), str):
                raise ValueError('invalid or duplicate block id / expectedText')
            if not isinstance(change.get('newText'), str) or len(change['newText']) > 60000:
                raise ValueError('newText invalid or too long')
            wanted[key] = change
        replacements = {}
        cells = []
        found = set()
        for block, tree, element in blocks(archive, kind):
            change = wanted.get(block['id'])
            if change is None:
                continue
            if not block['editable'] or block['text'] != change['expectedText']:
                raise ValueError('block changed or contains complex objects: ' + block['id'])
            found.add(block['id'])
            if kind == 'xlsx':
                cells.append({'sheet': block['sheet'], 'ref': block['cell'], 'value': change['newText']})
            else:
                # Every occurrence of a part shares the same parsed tree in this
                # generator; keep one tree per part so multiple edits accumulate.
                part = block['part']
                if part not in replacements:
                    replacements[part] = tree
                target = list(replacements[part].iter('{%s}p' % (W if kind == 'docx' else A)))[block['paragraph']]
                text_nodes = list(target.iter('{%s}t' % (W if kind == 'docx' else A)))
                text_nodes[0].text = change['newText']
                text_nodes[0].set(XMLSPACE, 'preserve')
                for node in text_nodes[1:]:
                    node.text = ''
        if found != set(wanted):
            raise ValueError('one or more block ids no longer exist')
        if kind == 'xlsx':
            edit_workbook(source, {'changes': cells}, output)
            return
        with zipfile.ZipFile(output, 'w') as result:
            for info in archive.infolist():
                data = ET.tostring(replacements[info.filename], encoding='UTF-8', xml_declaration=True,
                                   standalone=True) if info.filename in replacements else archive.read(info.filename)
                result.writestr(info, data)


if __name__ == '__main__':
    try:
        execute(sys.argv[1], sys.argv[2], sys.argv[3], json.loads(sys.argv[4]), sys.argv[5])
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
