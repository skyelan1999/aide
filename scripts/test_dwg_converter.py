#!/usr/bin/env python3
"""Exercise the shipped converter with pinned upstream SPLINE fixtures."""
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

import ezdxf

root = Path('/opt/aide/dwg')
metadata = json.loads((root/'source.json').read_text())
archive = root/'source'/('libredwg-'+metadata['version']+'.tar.xz')
assert hashlib.sha256(archive.read_bytes()).hexdigest() == metadata['sha256']
for patch in metadata.get('patches', []):
    assert hashlib.sha256((root/'source'/patch['file']).read_bytes()).hexdigest() == patch['sha256']
converter = os.environ.get('AIDE_DWG_CONVERTER', str(root/'bin/dwg2dxf'))
with tarfile.open(archive) as source, tempfile.TemporaryDirectory() as td:
    for revision in ('2000', '2004', '2007', '2010', '2013', '2018'):
        original = Path(td)/(revision+'.dwg')
        original.write_bytes(source.extractfile('libredwg-'+metadata['version']+'/test/test-data/'+revision+'/Spline.dwg').read())
        output = original.with_suffix('.dxf')
        subprocess.run([converter, '-o', str(output), str(original)], check=True, capture_output=True, timeout=20)
        splines = list(ezdxf.readfile(output).modelspace().query('SPLINE'))
        assert splines, revision+' has no curves'
        for curve in splines:
            points = list(curve.control_points) or list(curve.fit_points)
            assert len(points) >= 2 and all(math.isfinite(v) and abs(v)<1e12 for p in points for v in p), revision
            if len(curve.control_points):
                assert len(curve.knots) == len(points)+curve.dxf.degree+1, revision
                assert all(a<=b for a,b in zip(curve.knots,curve.knots[1:])), revision
            evaluated = list(curve.construction_tool().approximate(20))
            assert all(math.isfinite(v) and abs(v)<1e12 for p in evaluated for v in p), revision
        print('PASS upstream', revision, len(splines), 'valid evaluated SPLINEs')

# Pinned upstream gradient/pattern HATCH fixture, independent of customer data.
with tarfile.open(archive) as source, tempfile.TemporaryDirectory() as td:
    original, output = Path(td)/'hatch.dwg', Path(td)/'hatch.dxf'
    original.write_bytes(source.extractfile('libredwg-'+metadata['version']+'/test/test-data/2004/HatchG.dwg').read())
    subprocess.run([converter, '-o', str(output), str(original)], check=True, capture_output=True, timeout=20)
    hatches = list(ezdxf.readfile(output).modelspace().query('HATCH'))
    assert hatches and all(len(h.paths) for h in hatches)
    print('PASS upstream gradient/pattern HATCH boundary preservation', len(hatches))
