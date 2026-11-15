# DWG preview runtime component

`source.json` pins GNU LibreDWG 0.14 and the SHA-256 of its official source archive.
`scripts/build-dwg-converter.sh` builds only the separate `dwg2dxf` command used
for readonly temporary previews. Aide does not link against LibreDWG.

The runtime keeps `/opt/aide/dwg/COPYING` (GPL-3.0-or-later), `AUTHORS`, the exact
corresponding upstream source archive, its checksum, this manifest and the build
recipe. No proprietary ODA/Autodesk component is downloaded or redistributed.
Do not remove the source/license payload when packaging a runtime image.

Source builds require access to the pinned GNU URL on the first build. A verified
BuildKit source cache is reused afterward; a missing/invalid archive fails the
build rather than silently delivering a runtime without DWG support.

For a local rebuild with normal C build tools and Python 3 installed:

```sh
sh scripts/build-dwg-converter.sh /absolute/output/dwg docker/libredwg/source.json
```

Advanced CAD objects, externally referenced drawings, 3D solids and fonts may not
be represented by the existing 2D DXF viewer. Preview is not a CAD editing or
engineering-validation workflow. Original DWG files are never overwritten.

The manifest also pins `spline-scenario.patch`. It corrects R2013+ SPLINE
scenario selection: a fit-method flag only selects fit points when knot
parameterization is enabled. Without bit 8, the control-point definition is
retained (also reflected by LibreCAD/libdxfrw `DRW_Spline::parseDwgSplineBody`).
The patch is verified before applying and shipped alongside the upstream archive,
so the modified corresponding source is reproducible.

Additional pinned patches restore source geometry rather than suppressing preview warnings:

- `hatch-empty-fitpoints.patch`: an empty fit-point list is valid for a control-point HATCH spline edge; continue reading following boundaries and pattern definitions.
- `acds-sab-region.patch`: read bounded AcDs records using their stored object handles and preserve exact SAB/ASM bytes in R2013+ DXF ACDSDATA; avoid lossy SAT conversion. Only validated entity records are copied; unsupported schemas remain untouched.
- `proxy-binary-dxf.patch`: serialize the common entity's saved proxy preview as binary DXF chunks. Custom-object bytes are independent opaque data and must not replace the graphic, including when their pointer is absent.

The AcDs reader follows Open Design Specification section 24.2 (segment/record sizes and 16-byte object-data alignment), https://www.opendesign.com/files/guestdownloads/OpenDesign_Specification_for_.dwg_files.pdf. It caps section/copied data at 16 MiB and checks every segment, header, offset and length before reading. All patches and hashes are supplied with the corresponding source.

`dxf-mif-strings.patch` fixes Asian MIF codepage selection and terminates quoted output at the written cursor. Unmapped source escapes remain literal rather than becoming NUL. The converter build compiles the actual quoting function against its codepage tables and checks all five codepages, malformed escapes, output bounds and repeatability with nonzero buffer padding (`test_libredwg_strings.py`). The test script is included with corresponding source.
