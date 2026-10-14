'use strict';

let schwarzschildTable = null;

function schwarzschildDeflectionSample(rs, impactParameter) {
 const massRadius = Number.isFinite(rs) ? Math.max(0, rs) : 0;
 const impact = Number.isFinite(impactParameter) ? Math.max(0, impactParameter) : 0;
 if (!massRadius || !impact) return { angle: 0, derivative: 0 };
 const critical = 1.5 * Math.sqrt(3), ratio = impact / massRadius;
 if (ratio <= critical) return { angle: Infinity, derivative: -Infinity };
 const table = window.AideStarLensing._schwarzschildTable();
 if (ratio >= table.maxRatio) return { angle: 2 / ratio + 15 * Math.PI / (16 * ratio * ratio), derivative: -2 / (ratio * ratio) - 15 * Math.PI / (8 * ratio * ratio * ratio) };
 if (ratio <= table.minRatio) {
  const derivative = (table.values[1] - table.values[0]) / (table.critical * table.minOffset * table.logStep);
  return { angle: table.values[0], derivative };
 }
 const offset = ratio / table.critical - 1;
 const position = Math.log(offset / table.minOffset) / table.logStep, index = Math.min(table.values.length - 2, Math.floor(position)), fraction = position - index;
 const lower = table.values[index], upper = table.values[index + 1], slope = (upper - lower) / (table.critical * offset * table.logStep);
 return { angle: lower * (1 - fraction) + upper * fraction, derivative: slope };
}

// Schwarzschild null-geodesic deflection and projected lensing in normalized
// screen coordinates. The graph has no measured masses or distances, and the
// image projection remains thin-lens geometry rather than full spacetime ray tracing.
window.AideStarLensing = Object.freeze({
 // Schwarzschild null geodesics are scale invariant in b/rs. Build one
 // dimensionless lookup table from the exact turning-point integral, then
 // interpolate it for screen rays. This keeps frame-time lensing cheap while
 // retaining the logarithmic strong-deflection rise at the photon sphere.
 schwarzschildDeflection(rs, impactParameter) {
  // The first two weak-field terms provide a smooth asymptotic tail.
  return schwarzschildDeflectionSample(rs, impactParameter).angle;
 },
 _schwarzschildTable() {
  if (schwarzschildTable) return schwarzschildTable;
  const critical = 1.5 * Math.sqrt(3), minOffset = 1e-5, maxRatio = 96, count = 1024, steps = 256;
  const minRatio = critical * (1 + minOffset), maxOffset = maxRatio / critical - 1;
  const integrate = impact => {
   // The outer turning point solves 1 - b²(1-rs/r)/r² = 0. All values here
   // use rs=1; spherical symmetry makes the result depend only on b/rs.
   let low = 1.5, high = impact;
   for (let i = 0; i < 56; i++) {
    const middle = (low + high) * .5, potential = 1 - impact * impact * (1 - 1 / middle) / (middle * middle);
    if (potential > 0) high = middle; else low = middle;
   }
   const closest = (low + high) * .5, scale = impact / closest, derivative = -(scale * scale) * (2 - 3 / closest);
   const sample = t => {
    if (t === 0) return 2 * scale / Math.sqrt(Math.max(1e-18, -derivative));
    const u = 1 - t * t, potential = Math.max(1e-18, 1 - scale * scale * u * u * (1 - u / closest));
    return 2 * scale * t / Math.sqrt(potential);
   };
   let sum = sample(0) + sample(1);
   for (let i = 1; i < steps; i++) sum += (i & 1 ? 4 : 2) * sample(i / steps);
   return 2 * (sum / (3 * steps)) - Math.PI;
  };
  const values = new Float64Array(count), logStep = Math.log(maxOffset / minOffset) / (count - 1);
  for (let i = 0; i < count; i++) values[i] = integrate(critical * (1 + minOffset * Math.exp(logStep * i)));
  schwarzschildTable = Object.freeze({ critical, minOffset, minRatio, maxRatio, logStep, values });
  return schwarzschildTable;
 },
 schwarzschildImages(betaX, betaY, rs, observerDistance, sourceDistance, pixelsPerRadian, maxWindingOrder = 0) {
  const sourceRadius = Math.hypot(betaX, betaY), massRadius = Number.isFinite(rs) ? Math.max(0, rs) : 0;
  const dl = Number.isFinite(observerDistance) ? Math.max(0, observerDistance) : 0;
  const ds = Number.isFinite(sourceDistance) ? Math.max(0, sourceDistance) : 0;
  const pixels = Number.isFinite(pixelsPerRadian) ? Math.max(0, pixelsPerRadian) : 0;
  if (!sourceRadius || !massRadius || !dl || ds <= dl || !pixels) return [];
  const rsRadius = massRadius, criticalImpact = 1.5 * Math.sqrt(3) * rsRadius;
  const minimum = pixels * Math.atan(criticalImpact / dl) * (1 + 2e-5);
  const weakEinstein = Math.sqrt(2 * rsRadius * dl * (ds - dl) / ds) / dl * pixels;
  const maximum = Math.min(pixels * .35, Math.max(sourceRadius + weakEinstein * 8, minimum * 32, 24));
  if (!(maximum > minimum)) return [];
  const distanceWeight = pixels * (ds - dl) / ds, directionX = betaX / sourceRadius, directionY = betaY / sourceRadius;
  const deflectionAt = radiusPx => {
   const angle = radiusPx / pixels, tangent = Math.tan(angle), cosine = Math.cos(angle), sample = schwarzschildDeflectionSample(rsRadius, dl * tangent);
   return { angle: sample.angle, derivative: sample.derivative / rsRadius * dl / pixels / (cosine * cosine) };
  };
  const windingLimit = Number.isFinite(maxWindingOrder) ? Math.max(0, Math.min(1, Math.floor(maxWindingOrder))) : 1;
  const solve = (sign, windingOrder) => {
   const residual = radiusPx => { const ray = deflectionAt(radiusPx), effectiveDeflection = ray.angle - windingOrder * 2 * Math.PI; return { value: sign * radiusPx - sign * distanceWeight * effectiveDeflection - sourceRadius, derivative: sign * (1 - distanceWeight * ray.derivative) }; };
   let left = minimum, right = maximum, fLeft = residual(left).value, fRight = residual(right).value;
   if (!Number.isFinite(fLeft) || !Number.isFinite(fRight) || fLeft * fRight > 0) return null;
   // The weak-field image gives Newton a close first guess. Keep a bracket at
   // every step and fall back to bisection whenever the local derivative would
   // leave it; strong-field roots therefore retain the same bounded guarantee.
   const pointEinstein = Math.sqrt(Math.max(0, 2 * rsRadius * dl * (ds - dl) / ds)) / dl * pixels;
   const pointRoot = (Math.sqrt(sourceRadius * sourceRadius + 4 * pointEinstein * pointEinstein) + (sign > 0 ? sourceRadius : -sourceRadius)) * .5;
   // Relativistic images lie exponentially close to the critical curve. Seed
   // them with the Schwarzschild strong-deflection expansion rather than the
   // weak-field Einstein image, then use the exact interpolated integral for
   // the bounded Newton correction below.
   const criticalScreen = pixels * Math.atan(criticalImpact / dl);
   const strongDeflectionConstant = Math.log(216 * (7 - 4 * Math.sqrt(3))) - Math.PI;
   const targetDeflection = windingOrder ? windingOrder * 2 * Math.PI + (criticalScreen - sign * sourceRadius) / distanceWeight : 0;
   const epsilon = windingOrder ? Math.exp(Math.max(-40, Math.min(20, strongDeflectionConstant - targetDeflection))) : 0;
   const relativisticRoot = pixels * Math.atan(criticalImpact * (1 + epsilon) / dl);
   let radius = Math.min(right, Math.max(left, windingOrder ? relativisticRoot : pointRoot));
   for (let i = 0; i < (windingOrder ? 6 : 32); i++) {
    const sample = residual(radius), fRadius = sample.value;
    if (Math.abs(fRadius) < 1e-9 || right - left < 1e-11) break;
    if ((fLeft < 0) === (fRadius < 0)) { left = radius; fLeft = fRadius; }
    else { right = radius; fRight = fRadius; }
    const newton = radius - fRadius / sample.derivative;
    radius = Number.isFinite(newton) && Math.abs(sample.derivative) > 1e-10 && newton > left && newton < right ? newton : (left + right) * .5;
   }
   const derivative = 1 - distanceWeight * deflectionAt(radius).derivative;
   const magnification = Math.min(100, Math.abs(radius / sourceRadius / Math.max(1e-6, Math.abs(derivative))));
   const side = sign > 0 ? 'primary' : 'secondary';
   return { x: directionX * sign * radius, y: directionY * sign * radius,
    branch: windingOrder ? `relativistic-${side}-${windingOrder}` : side, windingOrder, magnification };
  };
  const images = [];
  for (let windingOrder = 0; windingOrder <= windingLimit; windingOrder++) {
   images.push(solve(1, windingOrder), solve(-1, windingOrder));
  }
  return images.filter(Boolean);
 },
 visibleSchwarzschildImages(betaX, betaY, rs, observerDistance, sourceDistance, pixelsPerRadian, innerRadius = 0, outerRadius = Infinity, maxWindingOrder = 0) {
  const inner = Number.isFinite(innerRadius) ? Math.max(0, innerRadius) : 0;
  const outer = Number.isFinite(outerRadius) ? Math.max(inner, outerRadius) : Infinity;
  return this.schwarzschildImages(betaX, betaY, rs, observerDistance, sourceDistance, pixelsPerRadian, maxWindingOrder).filter(image => {
   const radius = Math.hypot(image.x, image.y);
   return radius >= inner && radius <= outer;
  });
 },
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
