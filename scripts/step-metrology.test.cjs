'use strict';
const assert=require('node:assert/strict');
const cad=require('../internal/server/web/step-metrology.js');
const near=(a,b)=>assert.ok(Math.abs(a-b)<1e-8,`${a} != ${b}`);
const frame=cad.frameFromPoints([100,200,300],[100,210,300],[90,200,300]);
assert.deepEqual(frame.x,[0,1,0]);assert.deepEqual(frame.y,[-1,0,0]);
assert.deepEqual(cad.coordinates([98,203,304],frame),[3,2,4]);
const d=cad.distance([100,200,300],[98,203,304],frame);near(d.distance,Math.sqrt(29));assert.deepEqual(d.delta,[3,2,4]);
near(cad.angle([1,0,0],[0,0,0],[0,1,0]),90);
near(cad.angle([1,0,0],[0,0,0],[-1,0,0]),180);
assert.throws(()=>cad.frameFromPoints([0,0,0],[1,0,0],[2,0,0]),/共线/);
assert.throws(()=>cad.frameFromPoints([0,0,0],[0,0,0],[0,1,0]),/重合/);
assert.throws(()=>cad.frameFromPoints([0,0,NaN],[1,0,0],[0,1,0]),/有限/);
assert.throws(()=>cad.angle([0,0,0],[0,0,0],[1,0,0]),/重合/);
// Display centering is reversed before storage, never baked into calibration.
const original=[100010,200020,-300030],center=[100000,200000,-300000];
const displayed=original.map((v,i)=>v-center[i]);
assert.deepEqual(cad.coordinates(displayed.map((v,i)=>v+center[i]),null),original);
console.log('STEP metrology: right-handed frame, original coordinates, deltas, angles and degenerate picks pass');

const {fitPerspective}=require('../internal/server/web/step-preview.js');
for(const size of [[155,25,35],[31900,31900,1900],[1,1,10000]])for(const aspect of [.5,1,2.2]){
 const fit=fitPerspective(size,aspect,45),right=[Math.SQRT1_2,Math.SQRT1_2,0],d=fit.direction,up=[-d[2]*right[1],d[2]*right[0],d[0]*right[1]-d[1]*right[0]];
 for(const x of [-size[0]/2,size[0]/2])for(const y of [-size[1]/2,size[1]/2])for(const z of [-size[2]/2,size[2]/2]){
  const point=[x,y,z],dot=a=>point.reduce((n,v,i)=>n+v*a[i],0),depth=fit.distance-dot(d),vertical=Math.tan(Math.PI/8)*depth;
  assert.ok(depth>0);assert.ok(Math.abs(dot(up))<vertical);assert.ok(Math.abs(dot(right))<vertical*aspect);
 }
}
