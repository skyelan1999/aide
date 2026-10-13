'use strict';

// Lightweight radiometry for the map's normalized scene coordinates. Indexed
// relations provide a fictional source-power proxy; z is projection depth,
// not a parsec measurement. This keeps optical falloff coherent without
// claiming observational astrophysics.
window.AideStarOptics = Object.freeze({
 apparentIntensity(connectionCount, distance) {
  const physics = window.AideCosmicPhysics;
  const power = physics ? physics.powerProxy(connectionCount) : Math.log1p(Math.max(0, connectionCount || 0));
  const flux = physics ? physics.inverseSquareFlux(power + .1, distance) : (power + .1) / (4 * Math.PI * Math.max(.25, distance || 1) ** 2);
  // Display transfer compresses scene radiance to an sRGB-like perceptual
  // level; multiplying by 4π here would cancel geometric dilution and wash
  // dense fields into white. It changes display only, not the flux model.
  return Math.max(.12, Math.min(1, Math.sqrt(flux * 2.4)));
 }
});
