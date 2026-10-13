'use strict';

// Optics for the knowledge-map scene. Equations are physical; inputs are
// normalized graph proxies because the index has no measured stellar masses,
// distances, spectra, or orbital elements.
window.AideCosmicPhysics = Object.freeze({
 powerProxy(degree) {
  const links = Number.isFinite(degree) ? Math.max(0, degree) : 0;
  return Math.log1p(links);
 },
 massProxy(degree) {
  // A bounded dynamical proxy derived from graph energy. It is deliberately
  // separate from luminosity: more relations increase both, but not linearly.
  const energy = this.powerProxy(degree);
  return Math.max(.25, Math.min(3.5, .35 + Math.sqrt(energy) * .72));
 },
 keplerMeanMotion(mass, semiMajorAxis) {
  const m = Number.isFinite(mass) ? Math.max(.01, mass) : 1;
  const a = Number.isFinite(semiMajorAxis) ? Math.max(.25, semiMajorAxis) : 1;
  return Math.sqrt(m / (a * a * a));
 },
 keplerOrbit(meanAnomaly, eccentricity, mass, semiMajorAxis) {
  const e = Math.max(0, Math.min(.72, Number.isFinite(eccentricity) ? eccentricity : 0));
  const a = Number.isFinite(semiMajorAxis) ? Math.max(.25, semiMajorAxis) : 1;
  const mean = Number.isFinite(meanAnomaly) ? meanAnomaly : 0;
  // Newton iteration solves M = E - e sin(E); bounded fixed work keeps the
  // animation deterministic and inexpensive at every frame.
  let eccentric = mean;
  for (let i = 0; i < 6; i++) eccentric -= (eccentric - e * Math.sin(eccentric) - mean) / (1 - e * Math.cos(eccentric));
  const x = a * (Math.cos(eccentric) - e);
  const y = a * Math.sqrt(1 - e * e) * Math.sin(eccentric);
  const radius = Math.hypot(x, y);
  const mu = Number.isFinite(mass) ? Math.max(.01, mass) : 1;
  // Vis-viva equation: v² = μ(2/r - 1/a). This preserves the correct
  // periapsis/apoapsis speed change for the solved Kepler ellipse.
  const speed = Math.sqrt(Math.max(0, mu * (2 / Math.max(.01, radius) - 1 / a)));
  return { x, y, radius, eccentricAnomaly: eccentric, meanMotion: this.keplerMeanMotion(mass, a), orbitalSpeed: speed };
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
 planetIllumination(hostPower, orbitRadius, phaseAngle, albedo = .3) {
  const incident = this.inverseSquareFlux(hostPower, orbitRadius);
  const reflected = incident * this.lambertPhase(phaseAngle) * Math.max(0, Math.min(1, Number.isFinite(albedo) ? albedo : .3));
  // Filmic display mapping preserves a visible night side while retaining
  // monotonic response to incident and reflected radiance.
  const display = value => value <= 0 ? 0 : value / (value + .16);
  return { incidentFlux: incident, reflectedFlux: reflected, displayIllumination: display(reflected), equilibriumProxy: Math.pow(incident, .25) };
 },
 planetSurfaceRadiance(hostPower, orbitRadius, albedo, normalDotLight) {
  const reflectivity = Math.max(0, Math.min(1, Number.isFinite(albedo) ? albedo : .3));
  const diffuse = Math.max(0, Math.min(1, Number.isFinite(normalDotLight) ? normalDotLight : 0));
  const irradiance = this.inverseSquareFlux(hostPower, orbitRadius) * reflectivity * diffuse;
  return irradiance <= 0 ? 0 : irradiance / (irradiance + .16);
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
