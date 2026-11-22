"""Read bounded DXF DIMENSION evidence without interpreting design intent.

No source writes, geometry creation, unit conversion, network or engineering
acceptance. Run through an existing SDK that provides ezdxf. Rendered strings,
raw overrides, calculated measurements and witness points remain separate.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
from pathlib import Path
import re
import sys

MAX_SOURCE_BYTES = 64 * 1024 * 1024
MAX_DIMENSIONS = 4096
MAX_BLOCK_ENTITIES = 512
MAX_TEXT_BYTES = 4096
MAX_OUTPUT_BYTES = 8 * 1024 * 1024
POINT_NAMES = ('defpoint', 'defpoint2', 'defpoint3', 'defpoint4', 'defpoint5',
               'text_midpoint', 'insert')
NUMBER_NAMES = ('angle', 'oblique_angle', 'text_rotation', 'horizontal_direction')


def digest(path, max_bytes=None):
    h = hashlib.sha256()
    seen = 0
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            seen += len(chunk)
            if max_bytes is not None and seen > max_bytes:
                raise ValueError('input grew beyond read budget')
            h.update(chunk)
    return h.hexdigest()


def finite_value(value):
    """Reject unsupported/nonfinite values rather than silently returning zero."""
    if isinstance(value, (str, bytes, bool)):
        raise ValueError('not a numeric measurement')
    if isinstance(value, (int, float)):
        result = float(value)
        if not math.isfinite(result):
            raise ValueError('nonfinite numeric value')
        return result
    values = list(value)
    if not 1 <= len(values) <= 3:
        raise ValueError('unsupported measurement/point arity')
    return [finite_value(item) for item in values]


def bounded_text(value):
    text = str(value)
    if len(text.encode('utf-8')) > MAX_TEXT_BYTES:
        raise ValueError('dimension text exceeds byte budget')
    return text


def dimension_evidence(entity, doc):
    row = {'handle': str(entity.dxf.handle), 'type': 'DIMENSION',
           'rawText': None, 'renderedTexts': [], 'witnessPointsDrawingUnits': {},
           'parameters': {}, 'errors': [], 'complete': True}
    try:
        row['dimtype'] = int(entity.dxf.get('dimtype', 0))
        row['layer'] = bounded_text(entity.dxf.get('layer', ''))
        row['rawText'] = bounded_text(entity.dxf.get('text', '<>'))
        row['renderBlock'] = bounded_text(entity.dxf.get('geometry', ''))
        for name in POINT_NAMES:
            if entity.dxf.hasattr(name):
                row['witnessPointsDrawingUnits'][name] = finite_value(entity.dxf.get(name))
        for name in NUMBER_NAMES:
            if entity.dxf.hasattr(name):
                row['parameters'][name] = finite_value(entity.dxf.get(name))
        points = row['witnessPointsDrawingUnits']
        if 'defpoint2' in points and 'defpoint3' in points:
            a, b = points['defpoint2'], points['defpoint3']
            if not isinstance(a, list) or not isinstance(b, list) or len(a) != len(b):
                raise ValueError('incompatible witness point arity')
            row['witnessDelta3Minus2DrawingUnits'] = finite_value([v-u for u, v in zip(a, b)])
            row['witnessEuclideanDistanceDrawingUnits'] = finite_value(math.dist(a, b))
    except (AttributeError, TypeError, ValueError, OverflowError) as error:
        row['errors'].append({'stage': 'attributes', 'error': str(error)[:240]})
    try:
        row['calculatedMeasurementDrawingUnits'] = finite_value(entity.get_measurement())
    except Exception as error:
        row['calculatedMeasurementDrawingUnits'] = None
        row['errors'].append({'stage': 'calculatedMeasurement',
                              'error': type(error).__name__ + ': ' + str(error)[:240]})

    block_name = row.get('renderBlock')
    if not block_name or block_name not in doc.blocks:
        row['errors'].append({'stage': 'renderBlock', 'error': 'missing anonymous rendering block'})
    else:
        for index, item in enumerate(doc.blocks[block_name]):
            if index >= MAX_BLOCK_ENTITIES:
                row['errors'].append({'stage': 'renderBlock', 'error': 'entity budget exhausted'})
                break
            if item.dxftype() not in ('TEXT', 'MTEXT'):
                continue
            try:
                text = item.dxf.text if item.dxftype() == 'TEXT' else item.text
                record = {'handle': str(item.dxf.handle), 'type': item.dxftype(),
                          'text': bounded_text(text)}
                if item.dxf.hasattr('insert'):
                    record['insertDrawingUnits'] = finite_value(item.dxf.insert)
                row['renderedTexts'].append(record)
            except Exception as error:
                row['errors'].append({'stage': 'renderedText',
                                      'error': type(error).__name__ + ': ' + str(error)[:240]})
        if not row['renderedTexts']:
            row['errors'].append({'stage': 'renderBlock', 'error': 'no TEXT/MTEXT labels found'})
    row['complete'] = not row['errors']
    return row


def inspect_document(doc, requested=None, max_dimensions=1024):
    if not 1 <= max_dimensions <= MAX_DIMENSIONS:
        raise ValueError('max-dimensions must be in [1,4096]')
    requested = {str(item).upper() for item in (requested or [])}
    if len(requested) > MAX_DIMENSIONS:
        raise ValueError('too many requested handles')
    rows, found, reasons = [], set(), []
    encoded_bytes = 16384  # reserve bounded header/error/handle metadata
    for entity in doc.modelspace():
        if entity.dxftype() != 'DIMENSION':
            continue
        handle = str(entity.dxf.handle).upper()
        if requested and handle not in requested:
            continue
        if len(rows) >= max_dimensions:
            reasons.append('dimension budget exhausted; selected evidence incomplete')
            break
        row = dimension_evidence(entity, doc)
        row_bytes = len(json.dumps(row, ensure_ascii=False, allow_nan=False).encode('utf-8'))
        if encoded_bytes + row_bytes > MAX_OUTPUT_BYTES:
            reasons.append('output budget exhausted; selected evidence incomplete')
            break
        encoded_bytes += row_bytes
        found.add(handle)
        rows.append(row)
    missing = sorted(requested - found)
    if missing:
        reasons.append('requested DIMENSION handles absent or not visited')
    if any(not row['complete'] for row in rows):
        reasons.append('one or more dimension records have explicit errors')
    try:
        unit_code = int(doc.header.get('$INSUNITS', 0))
    except (TypeError, ValueError, OverflowError):
        unit_code = None
        reasons.append('invalid source $INSUNITS value')
    return {'status': 'COMPLETE_EVIDENCE' if not reasons else 'INCOMPLETE_EVIDENCE',
            'complete': not reasons, 'modelAccepted': False,
            'unitsCode': unit_code, 'convertedToMillimetres': False,
            'measurementUnit': 'original DXF drawing units; not inferred engineering dimensions',
            'selectedDimensions': len(rows), 'missingRequestedHandles': missing,
            'reasons': reasons, 'dimensions': rows,
            'boundary': 'Raw DIMENSION overrides, calculated values, witness points and anonymous '
                        'render-block text are independent source evidence. No automatic choice of '
                        'design value, coordinate/3D mapping, unit conversion or engineering acceptance. '
                        'Witness deltas/distances are raw geometric observations, not resolved dimensions. '
                        'Top-level modelspace DIMENSIONs only; block-contained dimensions, proxy '
                        'entities and external references are not traversed.'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--file', required=True)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--handles', help='Optional comma-separated DIMENSION handles')
    parser.add_argument('--max-dimensions', type=int, default=1024)
    args = parser.parse_args(argv)
    try:
        if not re.fullmatch(r'[0-9a-fA-F]{64}', args.sha256):
            raise ValueError('expected SHA256 must contain 64 hexadecimal characters')
        source, output = Path(args.file), Path(args.output)
        if source.suffix.lower() != '.dxf' or not source.is_file():
            raise ValueError('input must be an existing DXF file')
        if source.stat().st_size > MAX_SOURCE_BYTES:
            raise ValueError('source exceeds 64 MiB input budget')
        if output.exists() or source.resolve() == output.resolve():
            raise FileExistsError('refuse to overwrite input or existing report')
        expected = args.sha256.lower()
        if not 1 <= args.max_dimensions <= MAX_DIMENSIONS:
            raise ValueError('max-dimensions must be in [1,4096]')
        if digest(source, MAX_SOURCE_BYTES) != expected:
            raise ValueError('source SHA256 does not match expected value')
        handles = [] if not args.handles else args.handles.split(',')
        if any(not re.fullmatch(r'[0-9A-Fa-f]{1,32}', item) for item in handles):
            raise ValueError('handles must be comma-separated hexadecimal values')
        import ezdxf
        doc = ezdxf.readfile(str(source))
        result = inspect_document(doc, handles, args.max_dimensions)
        if digest(source, MAX_SOURCE_BYTES) != expected:
            raise ValueError('source changed during inspection; no report written')
        result.update(sourcePath=str(source), sourceSHA256=expected,
                      sourceUnchangedThroughCheck=True,
                      auditorSHA256=digest(__file__), ezdxfVersion=str(ezdxf.__version__))
        data = (json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + '\n').encode('utf-8')
        if len(data) > MAX_OUTPUT_BYTES:
            raise ValueError('report exceeds 8 MiB output budget; select fewer dimensions')
        output.parent.mkdir(parents=True, exist_ok=True)
        with output.open('xb') as stream:
            stream.write(data)
        print(json.dumps({key: value for key, value in result.items() if key != 'dimensions'}, ensure_ascii=False))
        return 0 if result['complete'] else 3
    except Exception as error:
        print(json.dumps({'status': 'NOT_RUN', 'modelAccepted': False,
                          'error': type(error).__name__ + ': ' + str(error)[:500]}, ensure_ascii=False))
        return 4


if __name__ == '__main__':
    raise SystemExit(main())
