"""Run with the Aide renderer image: python3 office_preview_layout_test.py."""

import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET

from openpyxl import Workbook
from openpyxl.styles import Alignment, PatternFill


class OfficePreviewLayoutTest(unittest.TestCase):
    def test_merged_text_and_row_geometry(self):
        workbook = Workbook()
        original = workbook.active
        original.title = 'Automatic'
        original.merge_cells('A1:D1')
        original['A1'] = '合并单元格\n多行标题'
        original['A1'].alignment = Alignment(wrap_text=True)
        original['A1'].fill = PatternFill(patternType='solid', fgColor='FFD965')
        original['A3'] = '可编辑内容'

        for name, rtl, hidden in [('Fixed', False, False), ('RTL with hidden columns', True, True)]:
            sheet = workbook.create_sheet(name)
            sheet.sheet_view.rightToLeft = rtl
            sheet.merge_cells('B2:F2')
            sheet['B2'] = 'Merged span test\nFixed row geometry'
            sheet['B2'].alignment = Alignment(wrap_text=True)
            sheet['B2'].fill = PatternFill(patternType='solid', fgColor='FFB524')
            sheet.row_dimensions[2].height = 40
            sheet['B4'] = '=IF(ISBLANK(F2),"Blank kept","Changed")'
            for column, width in zip('CDEF', (13, 14, 15, 16)):
                sheet.column_dimensions[column].width = width
            sheet.column_dimensions['E'].hidden = hidden
            sheet.column_dimensions['F'].hidden = hidden
            sheet.protection.sheet = True

        workbook.create_sheet('Control')['A1'] = 'CONTROL'
        hidden_sheet = workbook.create_sheet('Hidden')
        hidden_sheet.sheet_state = 'hidden'
        hidden_sheet['A1'] = 'hidden text'

        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            xlsx = directory / 'input.xlsx'
            workbook.save(xlsx)
            original_bytes = xlsx.read_bytes()
            subprocess.run([sys.executable, str(Path(__file__).with_name('office_preview.py')),
                            shutil.which('soffice'), str(xlsx), str(directory)], check=True, timeout=30)
            meta = json.loads((directory / 'meta.json').read_text())
            self.assertEqual([sheet['name'] for sheet in meta['sheets']], workbook.sheetnames[:-1])
            self.assertEqual(xlsx.read_bytes(), original_bytes)
            text = subprocess.check_output(['pdftotext', '-bbox-layout', str(directory / 'input.pdf'), '-'])

        pages = ET.fromstring(text).findall('.//{*}page')
        self.assertEqual(len(pages), 4)
        automatic, fixed, rtl_hidden, control = pages
        widths = [float(page.attrib['width']) for page in pages]
        self.assertGreater(widths[0], widths[3] * 3.5)
        self.assertGreater(widths[1], widths[2] * 1.5)
        self.assertGreater(widths[2], widths[3] * 3)
        self.assertEqual(fixed.attrib['height'], rtl_hidden.attrib['height'])

        for page, expected in [(automatic, ('合并单元格', '多行标题', '可编辑内容')),
                               (fixed, ('Merged span test', 'Fixed row geometry', 'Blank kept')),
                               (rtl_hidden, ('Merged span test', 'Fixed row geometry', 'Blank kept')),
                               (control, ('CONTROL',))]:
            words = page.findall('.//{*}word')
            rendered = ' '.join(''.join(word.itertext()) for word in words)
            for line in expected:
                self.assertIn(line, rendered)
            for word in words:
                self.assertLessEqual(float(word.attrib['xMax']), float(page.attrib['width']))


if __name__ == '__main__':
    unittest.main()
