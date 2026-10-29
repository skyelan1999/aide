# Org Chart local corrections

#419: validate Height <= 1e9 instead of repeating the width bound. Comparisons also reject nonfinite dimensions. #421: validate the box-parent hierarchy before BoxTree.Build (which may update descendant levels recursively), then validate generated visual-tree depth, node count and cycles before hierarchy/layout passes. Limits are 128 edges in depth and 10,000 nodes, including internal nodes. Source and its exact embedded copies in extensions.min.js, integrate.min.js and orgchart.min.js stay identical; licenses/assembly metadata are retained.

#422 was tested without a product change: actual Hanger2/Hanger4 multirow layouts run successfully, with finite nonoverlapping boxes and every connector retained. The vertical layout calls AdjustSpacer before the horizontal pass, so the reported zero-height assumption does not hold for the normal positive-size boxes. It is not counted as a fix.
