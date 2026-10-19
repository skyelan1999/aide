# URDF preview dependencies

URDFLoader UMD 0.10.5 is vendored without source modifications from the official npm package. It supports the existing Three.js r128 runtime (declared peer range >=0.105.0), so this feature does not upgrade the shared graphics stack. The STL and OrbitControls loaders already present remain unchanged. ColladaLoader and OBJLoader are the official Three.js 0.128.0 classic loaders, also unmodified.

Exact npm tarball URLs, SHA512 integrity values and shipped file SHA256 hashes are recorded in [provenance.json](provenance.json). Upstream: [URDF loaders](https://github.com/gkjohnson/urdf-loaders), [Three.js](https://github.com/mrdoob/three/tree/r128). URDFLoader licensing is [Apache-2.0](LICENSE); Three.js loaders use [MIT](../three-LICENSE.txt). Both tarballs were integrity checked before extraction. Runtime loads these assets locally and requires no npm install or CDN.

The product supplies its own bounded, scoped mesh and texture reads through the authenticated file API. Default vendor network loaders are not used for URDF mesh resources. Standard revolute/continuous/prismatic/fixed joints and mimic are supported. This loader version does not implement floating/planar joint manipulation; the UI labels those joints as initial-pose-only. Xacro must first be expanded into URDF; no macros or ROS commands are executed.
