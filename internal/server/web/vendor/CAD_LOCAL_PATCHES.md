# CAD preview budgets

STLLoader.js (Three.js r128, original license retained) now validates binary face declarations against available bytes and a 3,000,000-facet preview limit before allocating vertex, normal or color arrays (#539). The core Three.js bundle and ColladaLoader remain unchanged.

URDF DAE preflight counts all float_array values, including unused geometries, against 3,000,000 total finite values and verifies declared lengths without split-array expansion (#541). It checks containment/instance_node references iteratively for cycles, a maximum depth of 128 (including reverse-declared references), and at most 10,000 expanded scene nodes (#543). It caps direct and expanded DAE light instances at 16 before construction. An additional aggregate imported-light budget of 16 covers separate mesh assets, is idempotent, and commits only after successful validation (#544). The viewer's own ambient/directional lights are separate.

Limits are deliberate preview limits; oversized resources produce per-asset warnings. Validation covers actual local STL/Collada loaders, ordinary geometry/light controls, source-count aggregation, reference reuse, cycle/depth limits, aggregate asset light tracking and URDF WebGL UI regression. It does not claim every DAE feature or native GPU behavior is covered.
