# Propeller CAD & BEMT capability

Source: `plugins/propeller-aero/{index.js,worker.py,manifest.json,README.md}`. Protocol v1.2: `ctx.tool` → `api.runPython` → isolated standard-library worker. `propeller_analyze` is read-only. `propeller_reconstruct` turns returned artifact strings into existing `api.proposeWrite` records; it does not bypass Aide proposal approval.

Defaults corrected to official DJI Avata 360 3340S approximate diameter 83.1 mm, nominal 3.3×4.0 inch, four blades visually counted. Official support/shop locators and limitations are in generated report and SOURCE_RECORD.json. Old three-blade 127 mm concept is preserved as history, not treated as OEM baseline.

The worker fixes chord pitch rotation, closes each blade with ear-clipped caps, records edge multiplicity and actual component connectivity, and integrates 64 midpoint annuli with bracketed axial induction. Polar/chord/sweep are assumed; no hub/interface, duct, motor, absolute acoustics or CFD claim. Small CAD XY scaling is explicitly reported and is not silently treated as CFD coupling.

Runtime acceptance on 2026-10-07: UI shows enabled plugin, both tools executed through deployed-equivalent host in aide-aide-1; analyze 13090.240 RPM / 64.0898 W total shaft estimate; reconstruct 15 proposals / 4074289 content bytes / no target directory write. Source host bytes verified identical to deployed binary embedded host. No paid model conversation or formal test suite run.

Hot installation: package manifest/index/worker/README in ZIP; upload through Plugin panel. Package requires Python 3 and Node already available in Aide, no numerical dependency installation. Rollback: disable plugin through panel; archive generated reconstruction folder. Original geometry files remain unchanged.
