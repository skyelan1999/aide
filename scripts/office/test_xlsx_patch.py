import hashlib
from pathlib import Path
import tempfile
import unittest
import zipfile

from lxml import etree as ET
from openpyxl import Workbook, load_workbook
from openpyxl.styles import Alignment, Font, PatternFill, Protection
from xlsx_patch import edit_workbook, tag


class CellPatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.source = Path(self.temp.name) / 'source.xlsx'
        self.output = Path(self.temp.name) / 'changed.xlsx'
        book = Workbook(); sheet = book.active; sheet.title = '数据'
        sheet.merge_cells('A1:C1'); sheet['A1'] = '原标题'
        sheet['A1'].font = Font(bold=True, size=20)
        sheet['A1'].fill = PatternFill('solid', fgColor='FFFF00')
        sheet['B3'] = 12.5; sheet['B3'].number_format = '$#,##0.00'
        sheet['B3'].protection = Protection(locked=False)
        sheet['C4'] = '=SUM(1,2)'; sheet['D5'] = '旧值'
        sheet['D5'].alignment = Alignment(horizontal='right', text_rotation=15)
        book.save(self.source)
        # Opaque legacy drawing/custom parts must survive byte for byte.
        with zipfile.ZipFile(self.source, 'a') as archive:
            archive.writestr('xl/media/legacy.wmf', b'opaque synthetic legacy image payload')
            archive.writestr('customXml/item1.xml', b'<custom>preserve</custom>')

    def test_values_styles_and_opaque_parts_are_preserved(self):
        before = hashlib.sha256(self.source.read_bytes()).hexdigest()
        edit_workbook(self.source, {'changes': [
            {'sheet': '数据', 'ref': 'A1', 'value': '新标题'},
            {'sheet': '数据', 'ref': 'D5', 'value': '第一行\n第二行'},
            {'sheet': '数据', 'ref': 'B3', 'value': 42.75},
            {'sheet': '数据', 'ref': 'Z200', 'value': True},
        ]}, self.output)
        self.assertEqual(hashlib.sha256(self.source.read_bytes()).hexdigest(), before)
        with zipfile.ZipFile(self.source) as old, zipfile.ZipFile(self.output) as new:
            self.assertEqual(set(old.namelist()), set(new.namelist()))
            for name in old.namelist():
                if name not in {'xl/worksheets/sheet1.xml', 'xl/styles.xml', 'xl/workbook.xml'}:
                    self.assertEqual(old.read(name), new.read(name), name)
        book = load_workbook(self.output); self.addCleanup(book.close); sheet = book['数据']
        self.assertEqual(sheet['A1'].value, '新标题')
        self.assertIn('A1:C1', sheet.merged_cells)
        self.assertTrue(sheet['A1'].font.bold)
        self.assertEqual(sheet['A1'].fill.fgColor.rgb, '00FFFF00')
        self.assertEqual(sheet['B3'].value, 42.75)
        self.assertEqual(sheet['B3'].number_format, '$#,##0.00')
        self.assertFalse(sheet['B3'].protection.locked)
        self.assertEqual(sheet['D5'].value, '第一行\n第二行')
        self.assertTrue(sheet['D5'].alignment.wrap_text)
        self.assertEqual(sheet['D5'].alignment.horizontal, 'right')
        self.assertEqual(sheet['D5'].alignment.text_rotation, 15)
        self.assertEqual(sheet['C4'].value, '=SUM(1,2)')
        self.assertIs(sheet['Z200'].value, True)
        readonly = load_workbook(self.output, read_only=True); self.addCleanup(readonly.close)
        self.assertEqual((readonly.active.max_row, readonly.active.max_column), (200, 26))

    def test_invalid_batch_cannot_publish_partial_changes(self):
        for ref, value in [('B1', 'merged continuation'), ('XFE1', 1), ('D5', '\x00'),
                           ('D5', float('nan')), ('D5', 'x' * 32768)]:
            with self.subTest(ref=ref, valueType=type(value).__name__), self.assertRaises(ValueError):
                edit_workbook(self.source, {'changes': [
                    {'sheet': '数据', 'ref': 'A1', 'value': 'must not publish'},
                    {'sheet': '数据', 'ref': ref, 'value': value},
                ]}, self.output)
            self.assertFalse(self.output.exists())

    def test_shared_formula_anchor_edit_keeps_other_formulas(self):
        with zipfile.ZipFile(self.source) as archive:
            parts = {name: archive.read(name) for name in archive.namelist()}
        tree = ET.fromstring(parts['xl/worksheets/sheet1.xml']); data = tree.find(tag('sheetData'))
        for index in range(10, 13):
            row = ET.SubElement(data, tag('row'), r=str(index))
            cell = ET.SubElement(row, tag('c'), r='D' + str(index))
            formula = ET.SubElement(cell, tag('f'), t='shared', si='7')
            if index == 10:
                formula.set('ref', 'D10:D12'); formula.text = 'A10+1'
        parts['xl/worksheets/sheet1.xml'] = ET.tostring(tree)
        with zipfile.ZipFile(self.source, 'w') as archive:
            for name, value in parts.items(): archive.writestr(name, value)
        edit_workbook(self.source, {'changes': [{'sheet': '数据', 'ref': 'D10', 'value': '=7'}]}, self.output)
        book = load_workbook(self.output); self.addCleanup(book.close)
        self.assertEqual([book.active[f'D{i}'].value for i in range(10, 13)], ['=7', '=A11+1', '=A12+1'])


if __name__ == '__main__':
    unittest.main()
