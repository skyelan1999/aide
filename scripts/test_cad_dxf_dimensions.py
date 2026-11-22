"""Pure fake-DXF boundary controls; input code is never imported or executed."""
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import cad_dxf_dimensions as audit


class Attributes(dict):
    def __getattr__(self, key):
        try:
            return self[key]
        except KeyError:
            raise AttributeError(key)

    def hasattr(self, key):
        return key in self


class Entity:
    def __init__(self, kind='DIMENSION', handle='A1', measurement=9., **attrs):
        self.kind, self.measurement = kind, measurement
        self.dxf = Attributes(handle=handle, **attrs)
        self.text = attrs.get('text', '')

    def dxftype(self):
        return self.kind

    def get_measurement(self):
        if isinstance(self.measurement, Exception):
            raise self.measurement
        return self.measurement


class Document:
    def __init__(self, entities=None, blocks=None):
        self.entities = entities or [Entity(text='7', geometry='*D1',
                                           defpoint2=(0., 0., 0.), defpoint3=(0., 9., 0.))]
        self.blocks = {'*D1': [Entity('MTEXT', handle='B1', text=r'\A1;7')]} if blocks is None else blocks
        self.header = {'$INSUNITS': 0}

    def modelspace(self):
        return iter(self.entities)


class DimensionEvidenceTests(unittest.TestCase):
    def test_raw_override_render_and_measurement_stay_distinct(self):
        r = audit.inspect_document(Document())
        self.assertTrue(r['complete'])
        row = r['dimensions'][0]
        self.assertEqual(row['rawText'], '7')
        self.assertEqual(row['calculatedMeasurementDrawingUnits'], 9.)
        self.assertEqual(row['renderedTexts'][0]['text'], r'\A1;7')
        self.assertEqual(row['witnessPointsDrawingUnits']['defpoint3'], [0., 9., 0.])
        self.assertEqual(row['witnessDelta3Minus2DrawingUnits'], [0., 9., 0.])
        self.assertEqual(row['witnessEuclideanDistanceDrawingUnits'], 9.)
        self.assertEqual(r['unitsCode'], 0)
        self.assertFalse(r['convertedToMillimetres'])
        self.assertFalse(r['modelAccepted'])

    def test_failed_measurement_is_not_zero(self):
        r = audit.inspect_document(Document([Entity(geometry='*D1', measurement=RuntimeError('unsupported'))]))
        self.assertFalse(r['complete'])
        self.assertIsNone(r['dimensions'][0]['calculatedMeasurementDrawingUnits'])

    def test_missing_render_or_handles_are_explicit(self):
        r = audit.inspect_document(Document(blocks={}), ['A1', 'F2'])
        self.assertFalse(r['complete'])
        self.assertEqual(r['missingRequestedHandles'], ['F2'])
        self.assertTrue(r['dimensions'][0]['errors'])

    def test_dimension_budget_is_incomplete(self):
        doc = Document([Entity(handle='A1', geometry='*D1'), Entity(handle='A2', geometry='*D1')])
        r = audit.inspect_document(doc, max_dimensions=1)
        self.assertEqual(r['selectedDimensions'], 1)
        self.assertFalse(r['complete'])

    def test_render_block_budget_is_incomplete(self):
        doc = Document(blocks={'*D1': [Entity('TEXT', text='7')] * 513})
        self.assertFalse(audit.inspect_document(doc)['complete'])

    def test_nonfinite_witness_and_measurement_are_errors(self):
        doc = Document([Entity(geometry='*D1', defpoint=(0., float('nan'), 0.), measurement=float('inf'))])
        r = audit.inspect_document(doc)
        self.assertFalse(r['complete'])
        self.assertNotIn('defpoint', r['dimensions'][0]['witnessPointsDrawingUnits'])
        json.dumps(r, allow_nan=False)

    def test_output_budget_does_not_return_false_complete(self):
        with patch.object(audit, 'MAX_OUTPUT_BYTES', 16400):
            r = audit.inspect_document(Document())
        self.assertFalse(r['complete'])
        self.assertEqual(r['selectedDimensions'], 0)

    def test_cli_hash_mismatch_or_existing_output_writes_nothing(self):
        with tempfile.TemporaryDirectory() as td:
            source, out = Path(td)/'source.dxf', Path(td)/'report.json'
            source.write_bytes(b'inert fake source; never parsed')
            args = ['--file', str(source), '--sha256', '0'*64, '--output', str(out)]
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(audit.main(args), 4)
            self.assertFalse(out.exists())
            out.write_text('preserve')
            args[3] = audit.digest(source)
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(audit.main(args), 4)
            self.assertEqual(out.read_text(), 'preserve')
            self.assertEqual(source.read_bytes(), b'inert fake source; never parsed')


if __name__ == '__main__':
    unittest.main()
