'use strict';
const assert=require('node:assert/strict');
const cad=require('../internal/server/web/cad-measure.js');
const near=(a,b,tol=1e-7)=>assert.ok(Math.abs(a-b)<tol,`${a} != ${b}`);
const m=[19.40997556,0,0,-19.40997556,.2115316,326087.58945];
for(const p of [[0,0],[1000,0],[-7281.275,16400.1234]]){const q=cad.apply(cad.inverse(m),cad.apply(m,p));near(q[0],p[0]);near(q[1],p[1]);}
assert.throws(()=>cad.inverse([0,0,0,0,1,1]));
near(cad.calculate('distance',[[0,0],[300,400]]).length,500);
assert.deepEqual(cad.calculate('distance',[[0,0],[300,400]]),{length:500,dx:300,dy:400});
near(cad.calculate('length',[[0,0],[300,400],[600,0]]).length,1000);
near(cad.calculate('angle',[[0,0],[0,10],[10,10]]).angle,90);
assert.equal(cad.calculate('angle',[[1,1],[1,1],[2,2]]),null);
for(const offset of [0,1e9]){const p=[[0,0],[30,0],[30,40],[0,40]].map(p=>p.map(v=>v+offset));const r=cad.calculate('area',p);near(r.area,1200);near(r.perimeter,140);near(cad.calculate('area',p.reverse()).area,1200);}
assert.equal(cad.calculate('area',[[0,0],[10,10],[0,10],[8,0]]),null);
const circle=cad.calculate('circle',[[15,20],[10,25],[5,20]]);near(circle.radius,5);near(circle.diameter,10);near(circle.center[0],10);near(circle.center[1],20);
assert.equal(cad.calculate('circle',[[0,0],[1,1],[2,2]]),null);
assert.deepEqual(cad.calculate('coordinate',[[-12,400]]),{x:-12,y:400});
assert.equal(cad.calculate('distance',[[NaN,0],[1,1]]),null);
// Snap radius remains 10 CSS px regardless of viewport zoom.
for(const scale of [.001,1,200]){assert.deepEqual(cad.nearest([0,0],[[9/scale,0],[11/scale,0]],10/scale),[9/scale,0]);assert.equal(cad.nearest([0,0],[[11/scale,0]],10/scale),null);}
near(cad.unitMM[1],25.4);near(cad.unitMM[6],1000);
console.log('PASS CAD model transforms, distances, polygon area/perimeter, circles, angles, invalid geometry, units and screen-space snapping');
