"""Read-only millimetre STEP edge/p-curve diagnostics with the pinned OCP SDK.

No construction, healing, export, tolerance edits or reader-setting overrides.
Kernel maxima are numerical diagnostics, not formal mathematical error bounds
or model acceptance. Run through the project's cad_python.py launcher.
"""
import argparse
import hashlib
import json
import math
from pathlib import Path

MAX_BYTES = 128 * 1024 * 1024
MAX_PAIRS = 8192
CROSSCHECK_EPS_MM = 1e-10


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


def checked_source(path, expected):
    path = Path(path).absolute()
    if path.is_symlink() or not path.is_file():
        raise ValueError('source must be a regular non-symlink file')
    if path.suffix.lower() not in ('.step', '.stp'):
        raise ValueError('source must be STEP or STP')
    if not 0 < path.stat().st_size <= MAX_BYTES:
        raise ValueError('source exceeds the 128 MiB input budget or is empty')
    if len(expected) != 64 or any(c not in '0123456789abcdef' for c in expected):
        raise ValueError('expected SHA256 must be 64 lowercase hexadecimal characters')
    if digest(path) != expected:
        raise ValueError('source SHA256 mismatch')
    return path


def check_numeric_result(done, error, distance, parameter, first, last, sampled_max):
    if not done or error != 0:
        raise ValueError('kernel calculation incomplete or failed: ' + str(error))
    if not all(math.isfinite(x) for x in (distance, parameter, first, last, sampled_max)):
        raise ValueError('non-finite kernel result or parameter')
    if distance < 0 or sampled_max < 0 or last <= first:
        raise ValueError('negative distance or invalid range')
    if parameter < first - 1e-9 or parameter > last + 1e-9:
        raise ValueError('kernel maximum parameter is outside the edge range')
    if distance + CROSSCHECK_EPS_MM < sampled_max:
        raise ValueError('kernel maximum underestimates an observed sample')


def inspect_step(path, expected, max_pairs=2048):
    if not isinstance(max_pairs, int) or isinstance(max_pairs, bool) or not 1 <= max_pairs <= MAX_PAIRS:
        raise ValueError('max-pairs must be between 1 and 8192')
    path = checked_source(path, expected)
    # Import only after input guards. Importing this module is inert on hosts
    # without OCP; this function needs the project's pinned CAD launcher.
    import OCP
    from OCP.Adaptor3d import Adaptor3d_CurveOnSurface
    from OCP.BRep import BRep_Tool
    from OCP.BRepAdaptor import BRepAdaptor_Curve, BRepAdaptor_Surface
    from OCP.Geom2dAdaptor import Geom2dAdaptor_Curve
    from OCP.GeomLib import GeomLib_CheckCurveOnSurface
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.Interface import Interface_Static
    from OCP.STEPControl import STEPControl_Reader
    from OCP.TopAbs import TopAbs_FACE, TopAbs_EDGE
    from OCP.TopExp import TopExp_Explorer
    from OCP.TopoDS import TopoDS
    from OCP.collections import Sequence_TCollection_AsciiString

    reader = STEPControl_Reader()
    if reader.ReadFile(str(path)) != IFSelect_RetDone:
        raise ValueError('STEP reader failed')
    lengths, angles, solids = (Sequence_TCollection_AsciiString() for _ in range(3))
    reader.FileUnits(lengths, angles, solids)
    units = [lengths.Value(i).ToCString() for i in range(1, lengths.Length() + 1)]
    if not units or any(unit.lower() not in ('mm', 'millimetre', 'millimeter') for unit in units):
        raise ValueError('this diagnostic currently supports millimetre STEP files only')
    internal_unit = str(Interface_Static.CVal_s('xstep.cascade.unit'))
    if internal_unit.upper() != 'MM' or abs(reader.SystemLengthUnit() - 1.0) > 1e-12:
        raise ValueError('reader internal unit is not confirmed millimetres')
    if reader.TransferRoots() <= 0:
        raise ValueError('STEP transfer produced no roots')
    shape = reader.OneShape()
    if shape.IsNull():
        raise ValueError('STEP transfer returned a null shape')

    rows, reasons = [], []
    faces = TopExp_Explorer(shape, TopAbs_FACE)
    face_index = 0
    exhausted = False
    while faces.More() and not exhausted:
        face = TopoDS.Face(faces.Current())
        edges = TopExp_Explorer(face, TopAbs_EDGE)
        edge_index = 0
        while edges.More():
            if len(rows) >= max_pairs:
                reasons.append('pair_budget_reached')
                exhausted = True
                break
            edge = TopoDS.Edge(edges.Current())
            row = {'faceIndex0': face_index, 'edgeInFaceIndex0': edge_index,
                   'orientation': str(edge.Orientation()), 'status': 'ERROR'}
            try:
                if BRep_Tool.Degenerated_s(edge):
                    row['status'] = 'DEGENERATE_EXCLUDED'
                else:
                    tol = float(BRep_Tool.Tolerance_s(edge))
                    row['storedEdgeToleranceMm'] = tol
                    if not math.isfinite(tol) or tol <= 0:
                        raise ValueError('invalid stored edge tolerance')
                    row['sameParameter'] = bool(BRep_Tool.SameParameter_s(edge))
                    row['sameRange'] = bool(BRep_Tool.SameRange_s(edge))
                    if not (row['sameParameter'] and row['sameRange']):
                        raise ValueError('edge is not comparable under common parameterization')
                    curve = BRepAdaptor_Curve(edge)
                    first, last = float(curve.FirstParameter()), float(curve.LastParameter())
                    if not math.isfinite(first) or not math.isfinite(last) or last <= first:
                        raise ValueError('invalid edge parameter interval')
                    pc = BRep_Tool.CurveOnSurface_s(edge, face, 0.0, 0.0)
                    if pc is None:
                        raise ValueError('missing p-curve')
                    on_surface = Adaptor3d_CurveOnSurface(
                        Geom2dAdaptor_Curve(pc, first, last), BRepAdaptor_Surface(face))
                    check = GeomLib_CheckCurveOnSurface(curve)
                    check.SetParallel(False)
                    check.Perform(on_surface)
                    done, error = bool(check.IsDone()), int(check.ErrorStatus())
                    row.update(kernelDone=done, kernelErrorStatus=error)
                    if not done or error:
                        raise ValueError('kernel calculation failed or incomplete')
                    distance, parameter = float(check.MaxDistance()), float(check.MaxParameter())
                    sampled = max(float(curve.Value(first + (last-first)*j/32).Distance(
                        on_surface.Value(first + (last-first)*j/32))) for j in range(33))
                    check_numeric_result(done, error, distance, parameter, first, last, sampled)
                    row.update(status='MEASURED', range=[first, last], kernelDistanceMm=distance,
                               maxParameter=parameter, sample33MaxMm=sampled,
                               exceedsStoredTolerance=distance > tol, ratio=distance / tol)
            except Exception as exc:
                row['error'] = str(exc)[:2000]
                reasons.append('pair_calculation_error')
            rows.append(row)
            edges.Next()
            edge_index += 1
        faces.Next()
        face_index += 1
    if digest(path) != expected:
        reasons.append('source_changed_during_check')
    measured = [row for row in rows if row['status'] == 'MEASURED']
    if not measured:
        reasons.append('no_measurable_pairs')
    return {'status': 'COMPLETE_DIAGNOSTIC' if not reasons else 'INCOMPLETE_DIAGNOSTIC',
            'modelAccepted': False, 'sourcePath': str(path), 'sourceSHA256': expected,
            'sourceUnchangedThroughCheck': 'source_changed_during_check' not in reasons,
            'sdk': OCP.__version__, 'actualFileUnits': units, 'internalUnit': internal_unit,
            'complete': not reasons, 'reasons': sorted(set(reasons)),
            'pairOccurrencesVisited': len(rows), 'measuredPairs': len(measured),
            'degeneratePairsExcluded': sum(row['status'] == 'DEGENERATE_EXCLUDED' for row in rows),
            'errorPairs': sum(row['status'] == 'ERROR' for row in rows),
            'aboveStoredTolerancePairs': sum(row['exceedsStoredTolerance'] for row in measured),
            'maxKernelDistanceMm': max((row['kernelDistanceMm'] for row in measured), default=None),
            'maxRatio': max((row['ratio'] for row in measured), default=None),
            'rows': rows, 'auditorSHA256': digest(__file__),
            'boundary': 'Numerical GeomLib_CheckCurveOnSurface maxima with 33-point cross-checks. Not a formal mathematical bound, geometry repair, STEP validity certificate, source-dimension check or model acceptance. Seam occurrences are measured with their actual orientation. Use Aide timeout/RSS limits for native computations.'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--file', required=True)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--max-pairs', type=int, default=2048)
    parser.add_argument('--output')
    args = parser.parse_args(argv)
    output = Path(args.output) if args.output else None
    if output is not None and output.exists():
        print(json.dumps({'status': 'NOT_RUN', 'error': 'output already exists; refusing overwrite'}))
        return 4
    try:
        result = inspect_step(args.file, args.sha256, args.max_pairs)
    except Exception as exc:
        print(json.dumps({'status': 'NOT_RUN', 'error': str(exc), 'modelAccepted': False}))
        return 4
    if output is not None:
        data = (json.dumps(result, ensure_ascii=False, indent=2) + '\n').encode('utf-8')
        with output.open('xb') as stream:
            stream.write(data)
    summary = {key: value for key, value in result.items() if key != 'rows'}
    summary['worstPairs'] = sorted(
        (row for row in result['rows'] if row['status'] == 'MEASURED'),
        key=lambda row: row['ratio'], reverse=True)[:10]
    if output is not None:
        summary['report'] = {'path': str(output), 'sha256': digest(output)}
    print(json.dumps(summary, ensure_ascii=False))
    return 0 if result['complete'] else 3


if __name__ == '__main__':
    raise SystemExit(main())
