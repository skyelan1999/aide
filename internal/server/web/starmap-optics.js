'use strict';

// Lightweight radiometry for the map's normalized scene coordinates. Indexed
// relations provide a fictional source-power proxy; z is projection depth,
// not a parsec measurement. This keeps optical falloff coherent without
// claiming observational astrophysics.
window.AideStarOptics = Object.freeze({
 apparentIntensity(connectionCount, distance) {
  const links = Number.isFinite(connectionCount) ? Math.max(0, connectionCount) : 0;
  const d = Number.isFinite(distance) ? Math.max(.25, distance) : 1;
  const luminosity = .1 + Math.log2(links + 1) / 6;
  const inverseSquareFlux = luminosity / (d * d);
  return Math.max(.16, Math.min(1, Math.sqrt(inverseSquareFlux)));
 }
});
