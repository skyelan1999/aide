# Marked 12.0.2 local link-scanning fixes

The upstream version and MIT notice remain unchanged. The original minified artifact SHA-256 is `15fabce5b65898b32b03f5ed25e9f891a729ad4c0d6d877110a7744aa847a894`.

#420 backports the bounded reference search from upstream commit `c6a25bb` (`avoid O(n^2) scanning in reflinkSearch`). The unanchored search uses a block-label budget of 999 items and an inline structural-item budget of 999 while leaving ordinary text runs unbounded. Existing anchored reference tokenizers remain unchanged.

#430 backports the href rule from upstream PR #4013: an unquoted nonempty destination requires one character, and the valid empty destination is recognized only immediately before a closing parenthesis. This avoids repartitioning a long whitespace-only failed link.

Verification includes 63 before/after ordinary Markdown controls in default/GFM/pedantic modes, including empty destinations, references, escaped/nested labels, Unicode, 999-character labels and long plain-text link labels. Independent workers with 5-second termination bounds reproduce both original failures; browser and Node test the same packaged bundle. This does not claim all Marked performance reports or inputs are fixed.
