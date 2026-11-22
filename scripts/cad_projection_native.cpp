// Narrow Python binding for an OCCT output handle. No model/file operations.
#include <pybind11/pybind11.h>
#include <Standard_Handle.hxx>
#include <Standard_Failure.hxx>
#include <Geom_Curve.hxx>
#include <Geom_Surface.hxx>
#include <Geom2d_Curve.hxx>
#include <ShapeConstruct_ProjectCurveOnSurface.hxx>
#include <cmath>
#include <stdexcept>

PYBIND11_DECLARE_HOLDER_TYPE(T, occ::handle<T>, true);
namespace py = pybind11;

static occ::handle<Geom2d_Curve> project_returned(
    const occ::handle<Geom_Curve>& curve,
    const occ::handle<Geom_Surface>& surface,
    double first, double last, double precision) {
  if (curve.IsNull() || surface.IsNull() || !std::isfinite(first)
      || !std::isfinite(last) || !std::isfinite(last-first)
      || !(last > first) || !std::isfinite(precision) || !(precision > 0.0)) {
    throw std::invalid_argument("non-null geometry and finite positive range/precision required");
  }
  occ::handle<Geom2d_Curve> result;
  try {
    py::gil_scoped_release release;
    ShapeConstruct_ProjectCurveOnSurface projector;
    projector.Init(surface, precision);
    if (!projector.Perform(curve, first, last, result, precision, precision)
        || result.IsNull()) {
      throw std::runtime_error("OCCT projector did not return a curve");
    }
  } catch (const Standard_Failure& error) {
    throw std::runtime_error(error.what() ? error.what() : "OCCT projection failed");
  }
  return result;
}

PYBIND11_MODULE(_cad_projection_native, m) {
  py::module_::import("OCP.Geom");
  py::module_::import("OCP.Geom2d");
  m.doc() = "Return OCCT ShapeConstruct projection output handle; no CAD file/shape mutation";
  m.attr("occt_header_tag") = "V8_0_1";
  m.attr("pybind_internals_id") = PYBIND11_INTERNALS_ID;
  m.def("project_curve", &project_returned, py::arg("curve"), py::arg("surface"),
        py::arg("first"), py::arg("last"), py::arg("precision"),
        "Return the computed pcurve itself; caller must independently validate achieved error.");
  m.def("roundtrip_curve", [](const occ::handle<Geom_Curve>& curve) { return curve; },
        py::arg("curve"), "Synthetic cross-module holder interoperability check.");
}
