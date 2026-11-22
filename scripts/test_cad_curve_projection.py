"""Synthetic pinned-SDK tests; no boiler geometry or files are used."""
import math
import unittest
from cad_curve_projection import project_curve_on_surface, ProjectionError


class ArgumentGuards(unittest.TestCase):
    def test_invalid_budgets_before_kernel_import(self):
        for kw in ({'first':float('nan')}, {'last':float('inf')}, {'tolerance':0},
                   {'tolerance':True}, {'samples':16}, {'samples':258}, {'samples':True},
                   {'last':0}, {'max_nodes':16}, {'max_nodes':258}, {'max_nodes':True},
                   {'max_nodes':32769},
                   {'first':-1e308,'last':1e308}, {'use_native':1}):
            args = {'first':0., 'last':1., 'tolerance':1e-7, 'samples':65}
            args.update(kw)
            with self.subTest(kw=kw), self.assertRaises(ProjectionError):
                project_curve_on_surface(None, None, **args)


class SyntheticSDK(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        from OCP.Geom import Geom_Plane, Geom_Circle, Geom_CylindricalSurface
        from OCP.gp import gp_Pnt, gp_Dir, gp_Ax2, gp_Ax3
        cls.plane = Geom_Plane(gp_Pnt(0,0,0), gp_Dir(0,0,1))
        cls.circle = Geom_Circle(gp_Ax2(gp_Pnt(0,0,0),gp_Dir(0,0,1)),10)
        cls.cylinder = Geom_CylindricalSurface(gp_Ax3(gp_Pnt(0,0,0),gp_Dir(0,0,1)),10)

    def check_projection(self, curve, surface, last=2*math.pi):
        before = [(curve.Value(t).X(),curve.Value(t).Y(),curve.Value(t).Z())
                  for t in (0., last/3, last)]
        pcurve, receipt = project_curve_on_surface(curve,surface,0.,last,tolerance=1e-7)
        self.assertIsNotNone(pcurve)
        self.assertLessEqual(receipt['numericalMaxDistance'],1e-7)
        self.assertLessEqual(receipt['sampleMaxDistance'],1e-7)
        self.assertEqual(receipt['sampleCount'],65)
        self.assertFalse(receipt['modelAccepted'])
        self.assertFalse(receipt['formalErrorBound'])
        after = [(curve.Value(t).X(),curve.Value(t).Y(),curve.Value(t).Z())
                 for t in (0., last/3, last)]
        self.assertEqual(before,after)
        return pcurve, receipt

    def test_plane_circle_returns_usable_curve(self):
        pcurve,_ = self.check_projection(self.circle,self.plane)
        self.assertLess(self.circle.Value(.731).Distance(
            self.plane.Value(pcurve.Value(.731).X(),pcurve.Value(.731).Y())),1e-10)

    def test_periodic_cylinder_circle(self):
        self.check_projection(self.circle,self.cylinder)

    def test_explicit_higher_budget_preserves_original_accuracy(self):
        curve,surface=self.bspline_fixture()
        _, receipt=project_curve_on_surface(curve,surface,0.,2*math.pi,
                                           max_nodes=16385,use_native=False)
        self.assertLessEqual(receipt['numericalMaxDistance'],1e-7)
        self.assertLessEqual(receipt['sampleMaxDistance'],1e-7)
        self.assertFalse(receipt['edgesShapesTolerancesOrReaderSettingsModified'])
        self.assertEqual(receipt['samplePattern'],'endpoints_and_off_grid_interior')

    def test_off_grid_samples_reject_aliased_zero_kernel_maximum(self):
        # Deliberately emulate an underestimated numerical extremum. At all65
        # old uniform sample parameters this real129-pole zigzag lies on the
        # straight3D line; between them its displacement is0.001. A checker
        # must not accept those coincident knots as independent evidence.
        import OCP.GeomLib as geomlib
        from unittest.mock import patch
        from OCP.Geom import Geom_Line
        from OCP.Geom2d import Geom2d_BSplineCurve
        from OCP.collections import Array1_gp_Pnt2d,Array1_double,Array1_int
        from OCP.gp import gp_Pnt,gp_Dir,gp_Pnt2d
        from cad_curve_projection import _checked_projection
        points=Array1_gp_Pnt2d(1,129)
        knots=Array1_double(1,129)
        mults=Array1_int(1,129)
        for i in range(129):
            points.SetValue(i+1,gp_Pnt2d(i/128,.001 if i%2 else 0))
            knots.SetValue(i+1,i/128)
            mults.SetValue(i+1,2 if i in (0,128) else 1)
        pcurve=Geom2d_BSplineCurve(points,knots,mults,1,False)
        line=Geom_Line(gp_Pnt(0,0,0),gp_Dir(1,0,0))
        for i in range(65):
            uv=pcurve.Value(i/64)
            self.assertLess(line.Value(i/64).Distance(self.plane.Value(uv.X(),uv.Y())),1e-12)
        class MissedExtremum:
            def __init__(self,*args):pass
            def SetParallel(self,*args):pass
            def Perform(self,*args):pass
            def IsDone(self):return True
            def ErrorStatus(self):return 0
            def MaxDistance(self):return 0.
            def MaxParameter(self):return 0.
        with patch.object(geomlib,'GeomLib_CheckCurveOnSurface',MissedExtremum):
            with self.assertRaisesRegex(ProjectionError,'underestimates an observed distance'):
                _checked_projection(line,self.plane,pcurve,0.,1.,1e-7,65,
                                    'synthetic_aliased_extremum',1e-9,129)

    def bspline_fixture(self):
        from OCP.collections import Array2_gp_Pnt as TColgp_Array2OfPnt
        from OCP.Geom import Geom_BezierSurface, Geom_Circle
        from OCP.GeomConvert import GeomConvert
        from OCP.gp import gp_Pnt,gp_Dir,gp_Ax2
        poles=TColgp_Array2OfPnt(1,2,1,2)
        for i in (1,2):
            for j in (1,2):poles.SetValue(i,j,gp_Pnt(10*(i-1),10*(j-1),0))
        surface=GeomConvert.SurfaceToBSplineSurface_s(Geom_BezierSurface(poles))
        curve=Geom_Circle(gp_Ax2(gp_Pnt(5,5,0),gp_Dir(0,0,1)),2)
        return curve,surface

    def test_bspline_supporting_surface(self):
        curve,surface=self.bspline_fixture()
        self.check_projection(curve,surface)

    def test_insufficient_node_budget_rejected(self):
        curve,surface=self.bspline_fixture()
        with self.assertRaisesRegex(ProjectionError,'node budget exhausted'):
            project_curve_on_surface(curve,surface,0.,2*math.pi,max_nodes=17,use_native=False)

    def test_off_surface_geometry_rejected(self):
        from OCP.Geom import Geom_Circle
        from OCP.gp import gp_Pnt,gp_Dir,gp_Ax2
        circle=Geom_Circle(gp_Ax2(gp_Pnt(0,0,2),gp_Dir(0,0,1)),10)
        with self.assertRaises(ProjectionError):
            project_curve_on_surface(circle,self.plane,0.,2*math.pi)


if __name__=='__main__':unittest.main(verbosity=2)
