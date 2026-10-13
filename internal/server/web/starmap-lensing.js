'use strict';

// Thin point-mass gravitational lensing in normalized screen coordinates.
// Einstein radius is supplied by the caller as a visual scale; the star map
// has no physical masses or source distances, so this is an optical analogy,
// not a reconstruction of a real black hole or a ray-traced spacetime.
window.AideStarLensing = Object.freeze({
 pointMassImages(betaX, betaY, einsteinRadius) {
  const beta = Math.hypot(betaX, betaY), thetaE = Math.max(0, einsteinRadius);
  if (!Number.isFinite(beta) || !Number.isFinite(thetaE) || thetaE === 0 || beta < 1e-6) return [];
  const root = Math.sqrt(beta * beta + 4 * thetaE * thetaE), unitX = betaX / beta, unitY = betaY / beta;
  const primary = .5 * (beta + root), secondary = .5 * (beta - root);
  return [
   { x: unitX * primary, y: unitY * primary, branch: 'primary', magnification: .5 + (beta * beta + 2 * thetaE * thetaE) / (2 * beta * root) },
   { x: unitX * secondary, y: unitY * secondary, branch: 'secondary', magnification: Math.max(0, (beta * beta + 2 * thetaE * thetaE) / (2 * beta * root) - .5) }
  ];
 },
 visiblePointMassImages(betaX, betaY, einsteinRadius, innerRadius = 0, outerRadius = Infinity) {
  const inner = Number.isFinite(innerRadius) ? Math.max(0, innerRadius) : 0;
  const outer = Number.isFinite(outerRadius) ? Math.max(inner, outerRadius) : Infinity;
  return this.pointMassImages(betaX, betaY, einsteinRadius).filter(image => {
   const radius = Math.hypot(image.x, image.y);
   return radius >= inner && radius <= outer;
  });
 },
 deflection(betaX, betaY, einsteinRadius) {
  const beta2 = betaX * betaX + betaY * betaY, thetaE2 = einsteinRadius * einsteinRadius;
  if (!Number.isFinite(beta2) || beta2 < 1e-8 || !Number.isFinite(thetaE2)) return { x: 0, y: 0 };
  return { x: thetaE2 * betaX / beta2, y: thetaE2 * betaY / beta2 };
 }
});
