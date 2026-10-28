# Local MathJax 4.1.3 patches

The upstream version remains 4.1.3. Keep the existing upstream and mhchem licenses. Review and carry these local changes when replacing vendor assets; do not assume an upstream upgrade includes them.

- #392 / #394: `centerOver` and `UnderOverBracket` share an eight-level limit on their expensive nested reparsing. The counter belongs to the parser configuration and is restored in `finally`, including errors. This bounds resource use; it does not claim to make the upstream duplicated MathML representation linear. Ordinary formulas retain the original implementation.
- #393: a proof tree permits 64 inference rules. The counter belongs to its actual stack item, because nested argument parsing can reset temporary node lists. Rejected proofs clear pending inference/sequent balancing lists before returning a TeX error. This bounds the existing quadratic balancing path; it does not replace the layout algorithm.
- #395: mhchem scans group nesting before entering its recursive state machine and rejects nesting above 64; escaped braces do not count.
- #396: AsciiMath compilation applies the existing document Safe policy to the generated MathML tree, including classes and IDs. If the Safe handler creates an error root, that root is returned. The original MathItem root is restored during cleanup. TeX/MathML filtering and all Safe policy options remain intact.
- #397: identity matrices allow at most 32 rows/columns; placeholder matrices allow at most 64 per axis, 1,024 cells and an estimated expansion budget bounded by configured `maxBuffer` (at most 32 KiB). Checks precede array/string expansion. Ordinary matrix syntax, including `mqty`, is retained.

Run `node scripts/browser-mathjax-issue-review.cjs` with `AIDE_COMPONENT_EVIDENCE_DIR` pointing to an owned evidence directory. It uses local MathJax startup, the same initial loader list as Aide, TeX `require`, AsciiMath, Safe and SVG in Chromium. All requests stay on its owned test origin. It does not connect to users' workspaces or call a model.
