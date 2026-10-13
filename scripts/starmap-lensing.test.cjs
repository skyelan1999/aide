'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = { window: {} };
vm.runInNewContext(fs.readFileSync('internal/server/web/starmap-lensing.js', 'utf8'), context);
const lens = context.window.AideStarLensing;

assert.equal(lens.pointMassImages(0, 0, 3).length, 0);
const images = lens.pointMassImages(2, 0, 3);
assert.equal(images.length, 2);
assert.equal(images[0].branch, 'primary');
assert.equal(images[1].branch, 'secondary');
for (const image of images) {
 const signedTheta = Math.sign(image.x) * Math.hypot(image.x, image.y);
 assert.ok(Math.abs(signedTheta - 9 / signedTheta - 2) < 1e-10);
 assert.ok(Number.isFinite(image.magnification) && image.magnification >= 0);
}
const deflection = lens.deflection(4, 0, 3);
assert.ok(Math.abs(deflection.x - 2.25) < 1e-12);
assert.equal(deflection.y, 0);
const zeroDeflection = lens.deflection(0, 0, 3);
assert.equal(zeroDeflection.x, 0);
assert.equal(zeroDeflection.y, 0);
assert.equal(lens.pointMassImages(1, 1, 0).length, 0);
vm.runInNewContext(fs.readFileSync('internal/server/web/starmap-optics.js', 'utf8'), context);
const optics = context.window.AideStarOptics;
const nearBright = optics.apparentIntensity(16, 1);
const farBright = optics.apparentIntensity(16, 2);
assert.ok(nearBright > farBright);
assert.ok(optics.apparentIntensity(32, 1) > optics.apparentIntensity(1, 1));
assert.ok(optics.apparentIntensity(0, 1) > 0);
assert.ok(Number.isFinite(optics.apparentIntensity(8, 0)));
console.log('PASS starmap optics: inverse-square scene falloff, relation-driven source proxy, point-mass lens equation, deflection, singularity handling');
