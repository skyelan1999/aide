Use babeljs.io to convert for IE11, but disable it in IE11 as Math.hypot is missing.

Local sharp-corner correction (#249): use the current segment vector for the cap,
then rotate each side to the outgoing vector with complementary signed arcs.
Based on dotproto/perfect-freehand commit
042431852c5d92b5bf0e06f15a66df9ec9eec357 and its upstream #112 review;
use the full signed turn (not half the turn), with the positive/negative arc
handling from the follow-up review. Public API, normal-stroke controls, pressure,
taper and SVG adapter remain as before. See scripts/browser-freehand-corners.cjs
for actual path coverage in both turn directions and all quarter-turn rotations.
This is a local backport, not a claim of a new upstream release.
