"""Synthetic parser adapters; no Office libraries or user documents required."""
import importlib.util
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('bounded_extract', Path(__file__).parent/'office/extract_text.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class ExtractionBoundsTests(unittest.TestCase):
    def test_docx_stops_producing_paragraphs(self):
        seen = []
        def paragraphs():
            for i in range(10000):
                seen.append(i)
                yield SimpleNamespace(text='段'*256)
        doc = SimpleNamespace(paragraphs=paragraphs(), tables=[])
        with mock.patch.dict(sys.modules, {'docx':SimpleNamespace(Document=lambda _:doc)}):
            text = module.extract_docx('owned.docx')
        self.assertLess(len(text.encode()), 1<<20)
        self.assertLess(len(seen),10000)
        self.assertIn('truncated',text)

    def test_xlsx_closes_stream_after_truncation(self):
        seen = []; closed = []
        def rows(**_):
            for i in range(10000):
                seen.append(i)
                yield ('row', '界'*256)
        wb = SimpleNamespace(worksheets=[SimpleNamespace(title='Control',iter_rows=rows)],close=lambda:closed.append(True))
        with mock.patch.dict(sys.modules, {'openpyxl':SimpleNamespace(load_workbook=lambda *a,**k:wb)}):
            text = module.extract_xlsx('owned.xlsx')
        self.assertLess(len(text.encode()),1<<20)
        self.assertLess(len(seen),10000)
        self.assertEqual(closed,[True])

    def test_pdf_stops_producing_pages(self):
        seen = []
        def pages():
            for i in range(10000):
                seen.append(i)
                yield SimpleNamespace(extract_text=lambda:'pdf '*128)
        with mock.patch.dict(sys.modules, {'pypdf':SimpleNamespace(PdfReader=lambda _:SimpleNamespace(pages=pages()))}):
            text=module.extract_pdf('owned.pdf')
        self.assertLess(len(text.encode()),1<<20)
        self.assertLess(len(seen),10000)

    def test_pptx_positive_control_and_bounds(self):
        def slides(count):
            for _ in range(count):
                yield SimpleNamespace(shapes=[SimpleNamespace(has_text_frame=True,text_frame=SimpleNamespace(paragraphs=[SimpleNamespace(runs=[SimpleNamespace(text='slide '*128)])]))])
        with mock.patch.dict(sys.modules, {'pptx':SimpleNamespace(Presentation=lambda _:SimpleNamespace(slides=slides(10000)))}):
            self.assertLess(len(module.extract_pptx('owned.pptx').encode()),1<<20)
        with mock.patch.dict(sys.modules, {'pptx':SimpleNamespace(Presentation=lambda _:SimpleNamespace(slides=slides(1)))}):
            text=module.extract_pptx('small.pptx')
            self.assertIn('slide',text)
            self.assertNotIn('truncated',text)

if __name__=='__main__':unittest.main()
