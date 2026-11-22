"""Return a numerically checked OCP pcurve without editing edges or shapes.

Run with the project's pinned cad_python.py. This module is inert on import.
It does not read/export CAD files, change reader settings, heal topology or
update tolerances. Distances use the input geometry's own length unit.
"""
import math
import importlib


class ProjectionError(ValueError):
    """Projection is missing, incomplete, inconsistent or outside its budget."""


def project_curve_on_surface(curve, surface, first, last, tolerance=1e-7, samples=65, max_nodes=257, use_native=True):
    """Return (new_pcurve, diagnostics) after same-parameter distance checks.

    An optional pinned native bridge returns ShapeConstruct's actual output
    handle. GeomProjLib also returns its result handle explicitly. Some OCP bindings of
    ShapeConstruct.Perform return only bool and do not expose a replaced C++
    output handle to the Python caller. A success flag is therefore insufficient.
    Failed methods remain in the diagnostics; a success flag never replaces
    measured error. Numerical extrema plus finite samples are not a formal bound,
    trim-wire/periodic-branch selection, or engineering/model acceptance.
    Native calls need the caller's normal process time and memory limits.
    """
    for name, value in (('first', first), ('last', last), ('tolerance', tolerance)):
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
            raise ProjectionError(name + ' must be a finite real number')
    if last <= first or not math.isfinite(last-first) or tolerance <= 0:
        raise ProjectionError('positive parameter span and tolerance required')
    if not isinstance(samples, int) or isinstance(samples, bool) or not 17 <= samples <= 257:
        raise ProjectionError('samples must be an integer between 17 and 257')
    if (not isinstance(max_nodes, int) or isinstance(max_nodes, bool)
            or not 17 <= max_nodes <= 16385 or (max_nodes-1) & (max_nodes-2)):
        raise ProjectionError('max_nodes must be 2^n+1, between 17 and 16385')
    if curve is None or surface is None:
        raise ProjectionError('curve and surface are required')
    if not isinstance(use_native,bool):
        raise ProjectionError('use_native must be boolean')

    from OCP.GeomProjLib import GeomProjLib
    # Projection's approximation request is not its achieved error. Leave an
    # accuracy margin, then enforce the caller's original budget below.
    approximation_tolerance = float(tolerance) * .01
    if approximation_tolerance == 0:
        raise ProjectionError('tolerance is below the representable approximation range')
    attempts=[]
    if use_native:
        try:
            native = importlib.import_module('_cad_projection_native')
        except ModuleNotFoundError as error:
            if error.name != '_cad_projection_native':
                raise
        else:
            if native.occt_header_tag != 'V8_0_1':
                raise ProjectionError('native projection bridge header version mismatch')
            try:
                pcurve = native.project_curve(curve,surface,float(first),float(last),approximation_tolerance)
                result,diagnostic=_checked_projection(curve,surface,pcurve,first,last,tolerance,samples,
                                           'ShapeConstruct_native_returned_output_handle',approximation_tolerance,0)
                diagnostic['priorProjectionFailures']=attempts
                return result,diagnostic
            except (RuntimeError,ValueError) as error:
                attempts.append({'method':'ShapeConstruct_native_returned_output_handle','reason':str(error)})
    pcurve = GeomProjLib.Curve2d_s(curve, float(first), float(last), surface, approximation_tolerance)
    if pcurve is not None:
        try:
            result,diagnostic=_checked_projection(curve,surface,pcurve,first,last,tolerance,samples,
                                       'GeomProjLib.Curve2d_s_explicit_result',approximation_tolerance,0)
            diagnostic['priorProjectionFailures']=attempts
            return result,diagnostic
        except ProjectionError as error:
            attempts.append({'method':'GeomProjLib.Curve2d_s_explicit_result','reason':str(error)})
    # A successful direct approximation can still exceed its requested budget.
    # Recover UV values at the ORIGINAL 3D parameters, rather than reporting the
    # requested approximation tolerance as an achieved physical error.
    from OCP.ShapeAnalysis import ShapeAnalysis_Surface
    from OCP.Geom2dAPI import Geom2dAPI_Interpolate
    from OCP.collections import HArray1_gp_Pnt2d, HArray1_double
    from OCP.gp import gp_Pnt2d
    projector = ShapeAnalysis_Surface(surface)
    periods = [surface.UPeriod() if surface.IsUPeriodic() else None,
               surface.VPeriod() if surface.IsVPeriodic() else None]
    node_count,last_error = 17,'no usable direct projection'
    while node_count <= max_nodes:
        uv_points,parameters = [],[]
        for i in range(node_count):
            t=first+(last-first)*(i/(node_count-1))
            point=curve.Value(t)
            uv=projector.ValueOfUV(point,approximation_tolerance)
            xy=[uv.X(),uv.Y()]
            if not all(math.isfinite(v)for v in xy):
                raise ProjectionError('nearest projection returned non-finite parameters')
            if uv_points:
                for axis,period in enumerate(periods):
                    if period is not None:
                        xy[axis]+=round((uv_points[-1][axis]-xy[axis])/period)*period
            projected=surface.Value(*xy)
            gap=point.Distance(projected)
            if not math.isfinite(gap)or gap>tolerance:
                raise ProjectionError('3D curve is not within the requested supporting-surface budget')
            uv_points.append(xy);parameters.append(t)
        closed=math.dist(uv_points[0],uv_points[-1])<=1e-10
        count=node_count-1 if closed else node_count
        points=HArray1_gp_Pnt2d(1,count)
        params=HArray1_double(1,node_count)
        for i,xy in enumerate(uv_points[:count],1):points.SetValue(i,gp_Pnt2d(*xy))
        for i,t in enumerate(parameters,1):params.SetValue(i,t)
        interpolator=Geom2dAPI_Interpolate(points,params,closed,1e-12)
        interpolator.Perform()
        if not interpolator.IsDone():
            raise ProjectionError('parameter-preserving UV interpolation failed')
        candidate=interpolator.Curve()
        try:
            result,diagnostic=_checked_projection(curve,surface,candidate,first,last,tolerance,samples,
                                       'adaptive_original_parameter_UV_projection',approximation_tolerance,node_count)
            diagnostic['priorProjectionFailures']=attempts
            return result,diagnostic
        except ProjectionError as error:
            last_error=str(error)
        node_count=2*(node_count-1)+1
    raise ProjectionError('projection node budget exhausted: '+last_error+'; prior methods: '+repr(attempts))


def _checked_projection(curve,surface,pcurve,first,last,tolerance,samples,method,approximation_tolerance,nodes):
    from OCP.GeomAdaptor import GeomAdaptor_Curve, GeomAdaptor_Surface
    from OCP.Geom2dAdaptor import Geom2dAdaptor_Curve
    from OCP.Adaptor3d import Adaptor3d_CurveOnSurface
    from OCP.GeomLib import GeomLib_CheckCurveOnSurface
    c3 = GeomAdaptor_Curve(curve, first, last)
    c2 = Geom2dAdaptor_Curve(pcurve, first, last)
    supporting_surface = GeomAdaptor_Surface(surface)
    composed = Adaptor3d_CurveOnSurface(c2, supporting_surface)
    checker = GeomLib_CheckCurveOnSurface(c3)
    checker.SetParallel(False)
    checker.Perform(composed)
    if not checker.IsDone() or checker.ErrorStatus() != 0:
        raise ProjectionError('curve/surface distance calculation incomplete')
    distance, parameter = float(checker.MaxDistance()), float(checker.MaxParameter())
    if not math.isfinite(distance) or distance < 0 or not math.isfinite(parameter):
        raise ProjectionError('kernel returned a non-finite distance or parameter')
    if not first <= parameter <= last:
        raise ProjectionError('kernel maximum lies outside the requested range')
    observed = []
    # Dyadic interpolation grids contain every point of the old uniform65
    # check once sufficiently refined. Keep endpoints, but offset interior
    # checks away from those knots to detect between-node residuals as well.
    offset = (3-math.sqrt(5))/2
    for i in range(samples):
        fraction = 0. if i == 0 else (1. if i == samples-1 else (i+offset)/(samples-1))
        t = first + (last - first) * fraction
        uv = pcurve.Value(t)
        value = curve.Value(t).Distance(surface.Value(uv.X(), uv.Y()))
        if not math.isfinite(value) or value < 0:
            raise ProjectionError('non-finite sample distance')
        observed.append(value)
    sample_max = max(observed)
    # Never hide an observed violation behind a smaller numerical extremum.
    if sample_max > distance + max(1e-12, tolerance * 1e-3):
        raise ProjectionError('kernel maximum underestimates an observed distance')
    if max(distance, sample_max) > tolerance:
        raise ProjectionError('measured same-parameter distance exceeds requested tolerance: '
                              + repr(max(distance, sample_max)))
    return pcurve, {
        'method': method,
        'range': [first, last], 'requestedDistanceBudget': tolerance,
        'kernelApproximationRequest': approximation_tolerance,
        'interpolationNodes': nodes,
        'kernelDone': True, 'kernelErrorStatus': 0,
        'numericalMaxDistance': distance, 'maxParameter': parameter,
        'sampleCount': samples, 'sampleMaxDistance': sample_max,
        'samplePattern': 'endpoints_and_off_grid_interior',
        'distanceUnit': 'input geometry length unit',
        'scope': 'same-parameter numerical projection check only',
        'trimWireOrPeriodicBranchAccepted': False,
        'formalErrorBound': False, 'modelAccepted': False,
        'edgesShapesTolerancesOrReaderSettingsModified': False,
    }
