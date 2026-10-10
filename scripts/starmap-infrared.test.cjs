'use strict';
const assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
let pixels;
const raster={createImageData:(w,h)=>({data:new Uint8ClampedArray(w*h*4)}),putImageData:p=>pixels=p.data};
const sandbox={window:{},document:{createElement:()=>({getContext:()=>raster})},performance:{now:()=>0},Float32Array,Math,Number};
vm.createContext(sandbox);vm.runInContext(fs.readFileSync('internal/server/web/starmap-infrared.js','utf8'),sandbox);sandbox.AideInfrared=sandbox.window.AideInfrared;
const model=sandbox.AideInfrared;
assert(model.energy(32)>model.energy(1));assert(model.energy(0)>0);assert.equal(model.energy(8,2),model.energy(8,1)/4);
assert(model.kernel(0,8)>model.kernel(8,8));assert(model.kernel(8,8)>model.kernel(16,8));
for(const sigma of [5,12,45]){let integral=0;const dr=6*sigma/4096;for(let i=0;i<4096;i++){const r=(i+.5)*dr;integral+=2*Math.PI*r*model.density(r,sigma)*dr;}assert(Math.abs(integral-1)<1e-5);}
assert.equal(model.density(31,5),0);assert.equal(model.density(0,0),0);
assert(model.density(0,5)>model.density(0,45));
let pressed,draws=0,saves=0,restores=0;
const button={setAttribute:(_,v)=>pressed=v,querySelector:()=>({textContent:''})},note={};
const mode=model.create({button,note,redraw:()=>{},onChange:()=>{},motionEnabled:()=>false});
const ctx={save:()=>saves++,restore:()=>restores++,drawImage:()=>draws++};
mode.set(true);assert.equal(pressed,'true');assert.equal(note.hidden,false);
const render=points=>{mode.draw(ctx,points,400,300,100);return Array.from(pixels).filter((_,i)=>i%4===3).reduce((a,b)=>a+b,0);};
const single={x:200,y:150,degree:1,distance:1};
const weak=render([{...single,degree:0}]),strong=render([{...single,degree:32}]);assert(strong>weak*5);
const one=render([single]),overlap=render([single,single]);assert(overlap>one);
assert(render([{...single,distance:2}])<one);
assert.equal(render([]),0);assert.equal(render([{x:NaN,y:150,degree:32}]),0);
assert(render(Array.from({length:100},()=>single))>overlap); // no first-32 source truncation
assert.equal(saves,restores);mode.set(false);const before=draws;mode.draw(ctx,[single],400,300,200);assert.equal(draws,before);
console.log('PASS infrared: degree energy, inverse-distance, radial decay, overlap, every source, empty sky, toggle/context');
