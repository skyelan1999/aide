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
