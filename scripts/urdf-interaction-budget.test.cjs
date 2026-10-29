'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),acorn=require('../internal/server/code_analysis/acorn.cjs');
const THREE=require('../internal/server/web/vendor/three.min.js');
const source=fs.readFileSync('internal/server/web/urdf-preview.js','utf8'),scope={window:{},TextEncoder,TextDecoder,DOMException};
vm.runInNewContext(source,scope);const api=scope.window.AideUrdfPreview,rows=[];
const fixtures=JSON.parse(fs.readFileSync('scripts/fixtures/urdf-texture-headers.json','utf8')).images;
function test(name,fn){try{rows.push({name,status:'PASS',detail:fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
function mesh(faces,z=0){const positions=new Float32Array(faces*9);for(let i=0;i<faces;i++)positions.set([-1,-1,0,1,-1,0,0,1,0],i*9);const geometry=new THREE.BufferGeometry();geometry.setAttribute('position',new THREE.BufferAttribute(positions,3));geometry.computeBoundingBox();const object=new THREE.Mesh(geometry,new THREE.MeshBasicMaterial({side:THREE.DoubleSide}));object.position.z=z;return object;}
function pick(objects){const robot=new THREE.Group();objects.forEach(o=>robot.add(o));robot.updateMatrixWorld(true);return api.nearestMeshPick(THREE,robot,new THREE.Raycaster(new THREE.Vector3(0,0,2),new THREE.Vector3(0,0,-1)));}
test('540 bounded nearest visible hit with actual r128 triangle intersections',()=>{
 const front=mesh(3000,.5),back=mesh(3000,0),hidden=mesh(20000,1);hidden.visible=false;
 for(const object of[front,back]){const actual=object.raycast;object.raycast=function(ray,sink){assert.equal(Array.isArray(sink),false,'no all-hit array to sort');return actual.call(this,ray,sink);};}
 const result=pick([back,front,hidden]);assert.equal(result.limited,false);assert.equal(result.hit.object,front);assert.equal(result.triangles,6000);return{testedTriangles:6000,retainedHits:1};
});
test('540 dense candidates and duplicate material groups reject before any triangle query',()=>{
 for(const objects of [[mesh(20000)],[mesh(6000),mesh(6000)]]){let calls=0;for(const object of objects)object.raycast=()=>{calls++;throw Error('owned triangle trap');};assert.equal(pick(objects).limited,true);assert.equal(calls,0);}
 const grouped=mesh(6000);grouped.material=[grouped.material];grouped.geometry.clearGroups();grouped.geometry.addGroup(0,18000,0);grouped.geometry.addGroup(0,18000,0);grouped.raycast=()=>{throw Error('owned duplicate-group trap');};assert.equal(pick([grouped]).limited,true);
});
test('540 off-ray dense bounds, invisible ancestors and transformed meshes',()=>{
 const dense=mesh(20000);dense.position.x=20;dense.raycast=()=>{throw Error('owned off-ray triangle trap');};const normal=mesh(1);assert.equal(pick([dense,normal]).hit.object,normal);
 const parent=new THREE.Group();parent.visible=false;parent.add(mesh(20000));assert.equal(pick([parent,normal]).limited,false);
 normal.scale.set(2,.5,3);normal.rotation.z=.2;assert.equal(pick([normal]).hit.object,normal);
});
test('542 real static PNG/JPEG/GIF/WebP lossy/lossless headers and animations',()=>{
 for(const image of fixtures){const bytes=Buffer.from(image.base64,'base64');if(image.animated)assert.throws(()=>api.textureDimensions(bytes,image.extension),/动画/);else{const size=api.textureDimensions(bytes,image.extension);assert.equal(size.width,8);assert.equal(size.height,8);assert.equal(size.pixels,64);}}
});
test('542 dimensions rejected before raster decode; malformed headers fail boundedly',()=>{
 for(const image of fixtures.filter(x=>!x.animated)){
  const bytes=Buffer.from(image.base64,'base64');
  for(const end of[0,1,Math.min(12,bytes.length-1)])assert.throws(()=>api.textureDimensions(bytes.subarray(0,end),image.extension));
 }
 const png=Buffer.from(fixtures.find(x=>x.extension==='png'&&!x.animated).base64,'base64');png.writeUInt32BE(4096,16);png.writeUInt32BE(4096,20);assert.throws(()=>api.textureDimensions(png,'png'),/像素/);
 const jpeg=Buffer.from(fixtures.find(x=>x.extension==='jpeg').base64,'base64'),sof=jpeg.indexOf(Buffer.from([255,192]));jpeg.writeUInt16BE(4096,sof+5);jpeg.writeUInt16BE(4096,sof+7);assert.throws(()=>api.textureDimensions(jpeg,'jpeg'),/像素/);
 const gif=Buffer.from(fixtures.find(x=>x.extension==='gif'&&!x.animated).base64,'base64');gif.writeUInt16LE(4096,6);gif.writeUInt16LE(4096,8);assert.throws(()=>api.textureDimensions(gif,'gif'),/像素/);
 // VP8L packs14-bit dimensions; alter only dimensions and keep encoded bytes small.
 const webp=Buffer.from(fixtures.find(x=>x.extension==='webp'&&x.mode==='RGBA'&&!x.animated).base64,'base64'),vp8l=webp.indexOf(Buffer.from('VP8L'));assert(vp8l>=0);webp.writeUInt32LE((4095|(4095<<14))>>>0,vp8l+9);assert.throws(()=>api.textureDimensions(webp,'webp'),/像素/);
});
test('542 inclusive per-preview pixel quota is shared and failure does not grow it',()=>{
 const budget=api.createTextureBudget();for(let i=0;i<4;i++)budget.reserve({width:2048,height:2048});assert.equal(budget.usedPixels,16777216);for(let i=0;i<10;i++)assert.throws(()=>budget.reserve({width:1,height:1}),/累计/);assert.equal(budget.usedPixels,16777216);assert.equal(api.createTextureBudget().usedPixels,0);
});
test('542 actual URL modifier charges every decode request and substitutes neutral before loading',()=>{
 const ast=acorn.parse(source,{ecmaVersion:'latest'});let callback;function walk(n){if(!n||typeof n!=='object')return;if(n.type==='CallExpression'&&n.callee.type==='MemberExpression'&&n.callee.property.name==='setURLModifier')callback=n.arguments[0];for(const v of Object.values(n))if(Array.isArray(v))v.forEach(walk);else if(v&&typeof v==='object')walk(v);}walk(ast);assert(callback);
 let warnings=0;const context={blobURLs:new Set(['blob:owned','blob:neutral']),textureDimensionsByURL:new Map([['blob:owned',{width:2048,height:2048,path:'fixture.png'}]]),textureBudgetWarned:new Set(),textureBudget:api.createTextureBudget(),warn(){warnings++;},neutralImage:()=> 'blob:neutral',uiTextUrdfPreview:k=>k};
 const resolve=vm.runInNewContext('('+source.slice(callback.start,callback.end)+')',context);for(let i=0;i<4;i++)assert.equal(resolve('blob:owned'),'blob:owned');assert.equal(resolve('blob:owned'),'blob:neutral');assert.equal(resolve('blob:owned'),'blob:neutral');assert.equal(warnings,1);assert.throws(()=>resolve('https://outside.invalid/image.png'));assert.equal(resolve('blob:neutral'),'blob:neutral');assert.equal(context.textureBudget.usedPixels,16777216);
});
console.log(JSON.stringify({checks:rows.length,rows}));assert(rows.every(r=>r.status==='PASS'));
