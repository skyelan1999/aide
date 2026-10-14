'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = { window: {} };
vm.runInNewContext(fs.readFileSync('internal/server/web/starmap-physics.js', 'utf8'), context);
const physics = context.window.AideCosmicPhysics;
const starmap = fs.readFileSync('internal/server/web/starmap.js', 'utf8');
const blackHoleRenderer = starmap.match(/function drawBlackHole\(hole,now,fade=1\)\{[\s\S]*?\n\}/)?.[0] || '';
assert.ok(blackHoleRenderer, 'black-hole renderer exists');
assert.match(blackHoleRenderer, /ctx\.scale\(1,view\)[\s\S]*?ctx\.restore\(\);ctx\.save\(\);ctx\.translate\(p\.x,p\.y\)/,
 'only the accretion disk is inclination-flattened; shadow and photon glow use circular screen space');
assert.match(blackHoleRenderer, /const bands=\[\[3,3\.8\],\[3\.8,5\.1\],\[5\.1,6\.8\],\[6\.8,9\]\],segments=112/,
 'the thin disk is drawn as a continuous, finely segmented annulus rather than separated arc strokes');
assert.match(blackHoleRenderer, /ctx\.arc\(0,0,rs\*outer,a,b\);ctx\.lineTo\(Math\.cos\(b\)\*rs\*inner,Math\.sin\(b\)\*rs\*inner\);ctx\.arc\(0,0,rs\*inner,b,a,true\);ctx\.closePath\(\);ctx\.fill\(\)/,
 'each disk wedge closes across its inner and outer radii to avoid dotted concentric rings');
assert.match(blackHoleRenderer, /createRadialGradient\(0,0,shadowR\*\.99,0,0,shadowR\*1\.12\)/,
 'photon glow is a separate radial optical layer around the computed shadow');
assert.match(blackHoleRenderer, /for\(let i=0;i<segments;i\+\+\)[\s\S]*?ctx\.arc\(0,0,shadowR\*1\.035,a,b\);ctx\.stroke\(\)/,
 'the near-critical photon ring is segmented by orbital Doppler brightness');
assert.doesNotMatch(blackHoleRenderer, /arc\(0,0,shadowR,-2\.72|arc\(0,0,shadowR,\.18/,
 'black-hole appearance has no arbitrary broken highlight arcs');
assert.ok(Math.abs(physics.inverseSquareFlux(12, 2) * 4 - physics.inverseSquareFlux(12, 1)) < 1e-12);
assert.ok(physics.displayRadiance(2, 1) > physics.displayRadiance(1, 1));
assert.ok(Math.abs(physics.beerLambert(2) - Math.exp(-2)) < 1e-12);
assert.ok(physics.henyeyGreenstein(1, .65) > physics.henyeyGreenstein(-1, .65), 'dust scatters forward more strongly');
assert.equal(physics.scatteringCosine({ x: 0, y: 0, z: 2 }, { x: 0, y: 0, z: 1 }), 1, 'a foreground dust screen scatters source light forward toward the observer');
assert.equal(physics.scatteringCosine({ x: 0, y: 0, z: 0 }, { x: 0, y: 0, z: 1 }), -1, 'reverse source geometry is back-scattering');
assert.equal(physics.scatteringCosine({ x: 1, y: 1, z: 1 }, { x: 1, y: 1, z: 1 }), 0, 'coincident source and dust have no defined angle');
assert.equal(physics.scatteringCosine({ x: NaN, y: 0, z: 1 }, { x: 0, y: 0, z: 1 }), 0, 'invalid scene vectors fail closed');
const dust = physics.dustTransfer(3, .7, .58, .2, .68);
assert.ok(dust.directRadiance < 3 && dust.scatteredRadiance > 0 && dust.absorbedRadiance > 0);
assert.ok(Math.abs(dust.energyResidual) < 1e-12, 'single-scattering transfer conserves energy');
assert.equal(physics.lambertPhase(0), 1);
assert.ok(physics.lambertPhase(Math.PI) < 1e-12);
assert.ok(physics.planetSurfaceRadiance(8, 1, .3, 1) > physics.planetSurfaceRadiance(8, 1, .3, .25));
assert.ok(physics.planetIllumination(8, 2, 0, .3).displayIllumination > physics.planetIllumination(8, 4, 0, .3).displayIllumination);
assert.ok(physics.schwarzschildRedshift(2, 8) > physics.schwarzschildRedshift(2, 4));
assert.ok(physics.dopplerFactor(.4, 1) > physics.dopplerFactor(.4, -1));
assert.ok(Math.abs(physics.orbitalBeta(2, 6) - .5) < 1e-12);
assert.equal(physics.orbitalBeta(2, 5.99), 0);
assert.equal(physics.schwarzschildRadius(2), 4);
assert.equal(physics.einsteinImpactParameter(2, 4, 4), 0, 'a source on the lens plane has no lensing distance');
assert.equal(physics.einsteinImpactParameter(2, 4, 3), 0, 'foreground sources are not lensed');
assert.ok(Math.abs(physics.einsteinImpactParameter(2, 4, 8) - Math.sqrt(8)) < 1e-12);
assert.ok(physics.projectedEinsteinRadius(2, 4, 8, 100) > physics.projectedEinsteinRadius(2, 4, 6, 100));
assert.equal(physics.projectedEinsteinRadius(2, 4, 3, 100), 0);
assert.ok(Math.abs(physics.projectedBlackHoleShadowRadius(2, 4, 100) - 1.5 * Math.sqrt(3) * 100) < 1e-12);
assert.equal(physics.projectedBlackHoleShadowRadius(2, 0, 100), 0);
for (const [radius, distance, pixels] of [[1.6, 1.25, 720], [4.2, 3.5, 1440], [7.5, 9, 2400]]) {
 const normalizedMass = physics.massLengthForProjectedShadowRadius(radius, distance, pixels);
 const projected = physics.projectedBlackHoleShadowRadius(normalizedMass, distance, pixels);
 assert.ok(Math.abs(projected - radius) < 1e-12, 'shadow calibration must round-trip through one normalized Schwarzschild mass');
}
assert.equal(physics.massLengthForProjectedShadowRadius(4, 0, 100), 0);
assert.ok(physics.diskView(1) > physics.diskView(0));
assert.ok(physics.diskPositionAngle(.5) > physics.diskPositionAngle(-.5));
assert.equal(physics.accretionEmission(2, 5, 0).intensity, 0);
const diskInner = physics.accretionEmission(2, 6, 0);
const diskPeak = physics.accretionEmission(2, 6 * 49 / 36, 0);
const diskOuter = physics.accretionEmission(2, 18, 0);
assert.equal(diskInner.temperature, 0, 'thin-disk inner boundary has zero emitted flux');
assert.ok(diskPeak.temperature > diskOuter.temperature, 'thin-disk temperature peaks outside the ISCO then cools outward');
assert.ok(physics.accretionEmission(2, 8, .8).intensity > physics.accretionEmission(2, 8, -.8).intensity, 'approaching disk material is Doppler boosted');
assert.ok(physics.accretionEmission(2, 8, .8).observedTemperature > physics.accretionEmission(2, 8, -.8).observedTemperature, 'Doppler shift changes observed color temperature');
assert.match(physics.temperatureColor(1.3), /^#[0-9a-f]{6}$/);
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
assert.equal(lens.visiblePointMassImages(2, 0, 3, 100, 120).length, 0, 'a source must not be suppressed if every optical image is outside the visible lens field');
assert.equal(lens.visiblePointMassImages(2, 0, 3, 0, 100).length, 2);
assert.equal(lens.visiblePointMassImages(2, 0, 3, 4, 100).length, 1, 'the event horizon can occult one image branch while leaving the other visible');
const deflection = lens.deflection(4, 0, 3);
assert.ok(Math.abs(deflection.x - 2.25) < 1e-12);
assert.equal(deflection.y, 0);
const zeroDeflection = lens.deflection(0, 0, 3);
assert.equal(zeroDeflection.x, 0);
assert.equal(zeroDeflection.y, 0);
assert.equal(lens.pointMassImages(1, 1, 0).length, 0);
const criticalImpact = 1.5 * Math.sqrt(3);
assert.equal(lens.schwarzschildDeflection(1, criticalImpact), Infinity, 'the critical impact parameter asymptotically approaches the photon sphere');
assert.ok(lens.schwarzschildDeflection(1, criticalImpact * (1 + 2e-5)) > 8, 'near-critical escaping rays accumulate a strong-field deflection');
assert.ok(Math.abs(lens.schwarzschildDeflection(1, criticalImpact * 1.001) - 6.51) < .03,
 'logarithmic sampling in distance from the critical curve resolves the Schwarzschild strong-deflection regime');
assert.ok(Math.abs(lens.schwarzschildDeflection(1, 100) - .0202945) < 2e-6, 'the exact geodesic curve approaches the expected weak-field limit');
assert.ok(Math.abs(lens.schwarzschildDeflection(3, 300) - lens.schwarzschildDeflection(1, 100)) < 1e-12, 'Schwarzschild bending is scale invariant in impact parameter / rs');
assert.deepEqual(Array.from(lens.schwarzschildImages(2, 0, .1, 1000, 2000, 1000), image => image.branch), ['primary', 'secondary'],
 'direct-image solving stays lightweight unless a caller explicitly requests photon-winding images');
const strongImages = lens.schwarzschildImages(2, 0, .1, 1000, 2000, 1000, 1);
assert.deepEqual(Array.from(strongImages, image => image.branch), ['primary', 'secondary', 'relativistic-primary-1', 'relativistic-secondary-1']);
for (const image of strongImages) {
 const signedRadius = Math.sign(image.x) * Math.hypot(image.x, image.y), impact = 1000 * Math.tan(Math.abs(signedRadius) / 1000);
 const deflection = lens.schwarzschildDeflection(.1, impact), effectiveDeflection = deflection - image.windingOrder * 2 * Math.PI;
 const predictedSource = signedRadius - Math.sign(signedRadius) * 1000 * .5 * effectiveDeflection;
 assert.ok(Math.abs(predictedSource - 2) < 1e-8, 'the image position solves the geodesic-deflection thin-lens equation');
 assert.ok(Number.isFinite(image.magnification) && image.magnification > 0);
 if (image.windingOrder) assert.ok(Math.abs(deflection - 2 * Math.PI) < .08,
  'first relativistic images follow escaping rays that complete one loop near the photon sphere');
}
assert.ok(Math.abs(Math.hypot(strongImages[2].x, strongImages[2].y) - Math.hypot(strongImages[3].x, strongImages[3].y)) < .1,
 'the first relativistic image pair approaches the apparent photon-ring radius from opposite sides');
for (const [betaX, betaY, rs, observerDistance, sourceDistance, pixels] of [
 [.7, -1.2, .04, 1200, 3500, 900],
 [12, 9, .6, 800, 2600, 1700],
 [26, -4, .2, 1700, 4000, 1300]
]) {
 const sourceRadius = Math.hypot(betaX, betaY), distanceRatio = (sourceDistance - observerDistance) / sourceDistance;
 for (const image of lens.schwarzschildImages(betaX, betaY, rs, observerDistance, sourceDistance, pixels)) {
  const sign = image.branch.includes('primary') ? 1 : -1, signedRadius = sign * Math.hypot(image.x, image.y);
  const impact = observerDistance * Math.tan(Math.abs(signedRadius) / pixels);
  const effectiveDeflection = lens.schwarzschildDeflection(rs, impact) - image.windingOrder * 2 * Math.PI;
  const predictedSource = signedRadius - sign * distanceRatio * pixels * effectiveDeflection;
  assert.ok(Math.abs(predictedSource - sourceRadius) < 2e-6, 'bracketed Newton roots remain accurate across scene mass, distance, and camera scales');
  assert.ok(Number.isFinite(image.magnification) && image.magnification >= 0);
 }
}
const weakScale = Math.sqrt(2 * .1 * 1000 * 1000 / 2000) / 1000 * 1000;
const weakImages = lens.pointMassImages(2, 0, weakScale);
assert.ok(Math.abs(strongImages[0].x - weakImages[0].x) < .1 && Math.abs(strongImages[1].x - weakImages[1].x) < .1,
 'geodesic images converge continuously to the familiar point-mass result in the weak-field regime');
assert.equal(lens.visibleSchwarzschildImages(2, 0, .1, 1000, 2000, 1000, 100, 120).length, 0,
 'the exact optical image is clipped by the shadow and lens-field boundaries');
vm.runInNewContext(fs.readFileSync('internal/server/web/starmap-optics.js', 'utf8'), context);
const optics = context.window.AideStarOptics;
const nearBright = optics.apparentIntensity(16, 1);
const farBright = optics.apparentIntensity(16, 2);
assert.ok(nearBright > farBright);
assert.ok(optics.apparentIntensity(32, 1) > optics.apparentIntensity(1, 1));
assert.ok(optics.apparentIntensity(0, 1) > 0);
assert.ok(Number.isFinite(optics.apparentIntensity(8, 0)));
console.log('PASS starmap optics: inverse-square scene falloff, relation-driven source proxy, Schwarzschild null-geodesic deflection, projected images, weak-field continuity, singularity handling');
