# STEP/STP optional preview

STEP preview is provided by the public [STEP Viewer plugin](https://github.com/skyelan1999/aide-pkg-STEPViewer). Aide's default image contains no OCCT runtime, STEP WebAssembly importer or CAD sidecar. Shared Three.js remains for STL and URDF; DXF remains on its independent dxf-parser/SVG path.

The plugin registers `.step` and `.stp` with the generic file-handler bridge. Original files are read-only and read in bounded blocks; the plugin retains the original native OpenCascade converter, units, face colors, assembly identity, metrology and explode controls. Native conversion runs locally with a 384 MiB input limit, bounded geometry, 15-minute deadline and a short lease which expires after the preview is abandoned. Limits do not guarantee arbitrary complex models will load.

The separate platform engine archive is downloaded only after clicking install, checked against the plugin's SHA-256/byte manifest, and installed outside the main image. It can be prepared offline. No provider credentials or model files go to the download server. Engine cache persists separately from plugin source; disabling the plugin removes its file handler, and core offers the original download rather than a bundled STEP renderer.

Default builds no longer prepare or copy CAD dependencies. Local dependency generation v2 rejects CAD-containing seed images: deleting a directory in a later Docker layer would retain all original bytes. Existing runtime/data directories are preserved for rollback; no running production service is stopped by source migration.

Historical native fidelity results remain in earlier task/review records. New optional-plugin and DXF/browser checks and actual release size comparison are tracked in [the extraction task](../tasks/step-public-plugin-20261023.json).
