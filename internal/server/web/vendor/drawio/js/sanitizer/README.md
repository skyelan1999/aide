# DOMPurify local backports

DOMPurify3.4.16, upstream Apache2.0/MPL2.0 header retained. Aide#425 replaces recursive template-fragment sanitization and SAFE_FOR_TEMPLATES text traversal with explicit work stacks. Shadow traversal hooks retain before/upon/after order; this does not claim to remove every shadow-root/native DOM recursion path.

Upstream bundle SHA256: `a701508f037a33500e987d2c555e5c9c0ae307bdd41e20b0b8110ef0a4eed07b`.
Patched bundle SHA256: `eb0f03f49dd28b31b2d39d23d8ba209962e4283f2a2ef9bbf05c70df0c0318a3`.

Real Chromium checks cover graph labels and both DOM input modes, with/without hooks and SAFE_FOR_TEMPLATES. The failure control explicitly uses a256KiB JavaScript stack budget; the default-stack6000/20000 cases passed on the original asset. See cycle-106.json for exact scope and evidence.
