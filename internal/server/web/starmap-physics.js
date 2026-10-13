'use strict';

// Optics for the knowledge-map scene. Equations are physical; inputs are
// normalized graph proxies because the index has no measured stellar masses,
// distances, spectra, or orbital elements.
window.AideCosmicPhysics = Object.freeze({
 powerProxy(degree) {
  const links = Number.isFinite(degree) ? Math.max(0, degree) : 0;
  return Math.log1p(links);
 },
 inverseSquareFlux(power, distance) {
  const p = Number.isFinite(power) ? Math.max(0, power) : 0;
  const d = Number.isFinite(distance) ? Math.max(.25, distance) : 1;
  return p / (4 * Math.PI * d * d);
 },
 lambertPhase(phaseAngle) {
  const phase = Math.max(0, Math.min(Math.PI, Number.isFinite(phaseAngle) ? phaseAngle : Math.PI / 2));
  return (Math.sin(phase) + (Math.PI - phase) * Math.cos(phase)) / Math.PI;
 },
 reflectedFlux(hostPower, orbitRadius, phaseAngle, albedo = .3) {
  const radius = Math.max(.1, Number.isFinite(orbitRadius) ? orbitRadius : 1);
  const reflectivity = Math.max(0, Math.min(1, Number.isFinite(albedo) ? albedo : .3));
  return this.inverseSquareFlux(hostPower, radius) * this.lambertPhase(phaseAngle) * reflectivity;
 },
 schwarzschildRedshift(rs, radius) {
  if (!Number.isFinite(rs) || !Number.isFinite(radius) || rs <= 0 || radius <= rs) return 0;
  return Math.sqrt(1 - rs / radius);
 },
 dopplerFactor(beta, lineOfSightCosine) {
  const speed = Math.max(0, Math.min(.999, Number.isFinite(beta) ? beta : 0));
  const sightline = Math.max(-1, Math.min(1, Number.isFinite(lineOfSightCosine) ? lineOfSightCosine : 0));
  return 1 / (Math.sqrt(1 - speed * speed) * (1 - speed * sightline));
 },
 diskView(pitch) {
  // The current map camera has a normalized pitch, not a 3D orientation matrix.
  // Convert that observable control into a bounded projected minor/major axis.
  const p = Math.max(-1, Math.min(1, Number.isFinite(pitch) ? pitch : 0));
  return Math.max(.22, Math.min(.78, .22 + Math.abs(p) * .56));
 },
 diskPositionAngle(yaw) {
  const angle = Number.isFinite(yaw) ? yaw : 0;
  return Math.max(-Math.PI * .48, Math.min(Math.PI * .48, angle * .42));
 },
 orbitalBeta(rs, radius) {
  // Local circular speed in Schwarzschild coordinates, valid for stable
  // circular orbits at/above the Schwarzschild ISCO (r = 3 rs).
  if (!Number.isFinite(rs) || !Number.isFinite(radius) || rs <= 0 || radius < 3 * rs) return 0;
  return Math.sqrt(rs / (2 * (radius - rs)));
 },
 accretionEmission(rs, radius, lineOfSightCosine) {
  if (!Number.isFinite(rs) || rs <= 0 || !Number.isFinite(radius) || radius < 3 * rs) {
   return { shift: 0, intensity: 0, beta: 0 };
  }
  // Circular-orbit speed measured by a local static observer in Schwarzschild
  // spacetime. The thin disk starts at the innermost stable circular orbit.
  const beta = this.orbitalBeta(rs, radius);
  const shift = this.schwarzschildRedshift(rs, radius) * this.dopplerFactor(beta, lineOfSightCosine);
  // Liouville's theorem preserves I_nu / nu^3 along a vacuum ray.
  const radialEmissivity = Math.pow(radius / (3 * rs), -3);
  return { shift, intensity: radialEmissivity * Math.pow(shift, 3), beta };
 },
 temperatureColor(shift) {
  const g = Math.max(.45, Math.min(1.8, Number.isFinite(shift) ? shift : 1));
  const mix = (a, b, t) => Math.round(a + (b - a) * Math.max(0, Math.min(1, t)));
  const lerp = (a, b, t) => [mix(a[0], b[0], t), mix(a[1], b[1], t), mix(a[2], b[2], t)];
  const color = g < .9 ? lerp([181, 73, 46], [255, 176, 111], (g - .45) / .45)
   : g < 1.15 ? lerp([255, 176, 111], [241, 246, 255], (g - .9) / .25)
    : lerp([241, 246, 255], [130, 190, 255], (g - 1.15) / .65);
  return '#' + color.map(value => value.toString(16).padStart(2, '0')).join('');
 }
});
