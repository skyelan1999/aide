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
  // The graph provides a luminosity proxy; the inverse-square flux stays
  // linear until this final camera exposure step.
  return Math.max(.08, Math.min(1, physics?.displayRadiance(flux, 4.8) ?? (1 - Math.exp(-flux * 4.8))));
 }
});
