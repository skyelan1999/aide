import json
from pathlib import Path
import tempfile
import unittest
import zipfile

from office_document import execute


class OfficeDocumentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'input.docx'
        self.output = self.root / 'output.docx'

    def document(self, body):
        with zipfile.ZipFile(self.source, 'w') as z:
            z.writestr('word/document.xml', '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>' + body + '</w:body></w:document>')
            z.writestr('word/media/opaque.bin', b'\x00keep exact opaque media\xff')

    def test_multiple_edits_preserve_opaque_parts_and_run_properties(self):
        self.document('<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>One</w:t></w:r></w:p><w:p><w:r><w:t>Two</w:t></w:r></w:p>')
        execute('edit', self.source, 'docx', {'changes': [
            {'id': 'word/document.xml#0', 'expectedText': 'One', 'newText': 'First'},
            {'id': 'word/document.xml#1', 'expectedText': 'Two', 'newText': 'Second'},
        ]}, self.output)
        with zipfile.ZipFile(self.output) as z:
            self.assertEqual(z.read('word/media/opaque.bin'), b'\x00keep exact opaque media\xff')
            text = z.read('word/document.xml')
            self.assertIn(b'First', text)
            self.assertIn(b'Second', text)
            self.assertIn(b'<w:b/>', text)

    def test_invalid_batch_never_writes_partial_output(self):
        self.document('<w:p><w:r><w:t>One</w:t></w:r></w:p>')
        with self.assertRaises(ValueError):
            execute('edit', self.source, 'docx', {'changes': [
                {'id': 'word/document.xml#0', 'expectedText': 'One', 'newText': 'Changed'},
                {'id': 'word/document.xml#9', 'expectedText': 'Missing', 'newText': 'No'},
            ]}, self.output)
        self.assertFalse(self.output.exists())

    def test_complex_field_is_readable_but_not_editable(self):
        self.document('<w:p><w:r><w:fldChar w:fldCharType="begin"/><w:t>Field</w:t></w:r></w:p>')
        execute('read', self.source, 'docx', {}, self.output)
        block = json.loads(self.output.read_text())['blocks'][0]
        self.assertEqual(block['text'], 'Field')
        self.assertFalse(block['editable'])

    def test_pagination_and_oversized_block_advance(self):
        self.document(''.join('<w:p><w:r><w:t>Block</w:t></w:r></w:p>' for _ in range(201)))
        execute('read', self.source, 'docx', {}, self.output)
        self.assertEqual(json.loads(self.output.read_text())['nextOffset'], 200)
        execute('read', self.source, 'docx', {'offset': 200}, self.output)
        result = json.loads(self.output.read_text())
        self.assertEqual(len(result['blocks']), 1)
        self.assertIsNone(result['nextOffset'])
        self.document('<w:p><w:r><w:t>' + 'x' * 70000 + '</w:t></w:r></w:p><w:p><w:r><w:t>Next</w:t></w:r></w:p>')
        execute('read', self.source, 'docx', {}, self.output)
        result = json.loads(self.output.read_text())
        self.assertEqual(result['nextOffset'], 1)
        self.assertTrue(result['blocks'][0]['textTruncated'])

    def test_collabora_nested_presentation_formatting_is_editable(self):
        with zipfile.ZipFile(self.source, 'w') as z:
            z.writestr('ppt/slides/slide1.xml', '<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:p><a:pPr><a:defRPr><a:latin typeface="Carlito"/></a:defRPr></a:pPr><a:r><a:rPr><a:solidFill><a:srgbClr val="000000"/></a:solidFill></a:rPr><a:t>Title</a:t></a:r></a:p></p:sld>')
        execute('read', self.source, 'pptx', {}, self.output)
        self.assertTrue(json.loads(self.output.read_text())['blocks'][0]['editable'])


if __name__ == '__main__':
    unittest.main()
