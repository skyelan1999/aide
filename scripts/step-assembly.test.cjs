'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const scope={};scope.window=scope;vm.runInNewContext(fs.readFileSync('internal/server/web/vendor/three.min.js','utf8'),scope);const THREE=scope.THREE;
const {directions,visible}=require('../internal/server/web/step-assembly.js');
function part(id,xyz){const center=new THREE.Vector3(...xyz);return{id,index:id,center,bounds:new THREE.Box3(center.clone().addScalar(-1),center.clone().addScalar(1))};}
const coincident=Array.from({length:30},(_,i)=>part(i,[0,0,0]));const radial=directions(coincident,'radial',THREE);assert.equal(new Set([...radial.values()].map(v=>v.toArray().join(','))).size,30);for(const v of radial.values())assert.ok(Math.abs(v.length()-1)<1e-10);
for(const axis of ['x','y','z']){const vectors=directions(coincident,axis,THREE),k={x:0,y:1,z:2}[axis];assert.equal(new Set([...vectors.values()].map(v=>v.getComponent(k))).size,30);for(const v of vectors.values())v.toArray().forEach((x,i)=>{if(i!==k)assert.equal(x,0);});}
const single=part(0,[100,200,300]);assert.equal(directions([single],'radial',THREE).get(0).length(),0);
const parent=new THREE.Group(),mesh=new THREE.Mesh(new THREE.BoxGeometry(1,1,1));parent.add(mesh);assert.equal(visible(mesh),true);parent.visible=false;assert.equal(visible(mesh),false);
console.log('PASS assembly radial/axis separation, coincident/single part handling, ancestor visibility');
