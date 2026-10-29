'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),acorn=require('../internal/server/code_analysis/acorn.cjs');
const source=fs.readFileSync('internal/server/web/app.js','utf8'),ast=acorn.parse(source,{ecmaVersion:'latest',sourceType:'script'});
const setup=ast.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='setupPdfPreview'),fn=setup.body.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='pdfPageMetrics');assert(fn);
const metrics=vm.runInNewContext('('+source.slice(fn.start,fn.end)+')');
const page=(width,height)=>({getViewport({scale}){return {width:width*scale,height:height*scale,transform:[scale,0,0,-scale,0,height*scale]};}});
let count=0;
for(const width of [0.001,1,200,612,8192,100000,1e100,1e300])for(const height of [0.001,1,200,792,8192,200000,1e100,1e300])for(const scale of [1e-300,0.001,0.4,1,3])for(const dpr of [1,2,20,1e200,NaN,Infinity,-1]){
 const m=metrics(page(width,height),scale,dpr);
 assert(m.width>=1&&m.height>=1&&m.width<=8192&&m.height<=8192&&m.width*m.height<=16777216);
 assert(m.cssWidth>=1&&m.cssHeight>=1&&m.cssWidth<=8192&&m.cssHeight<=8192);
 assert(Number.isFinite(m.outputScale)&&m.outputScale>0);count++;
}
for(const dpr of [1,2]){const m=metrics(page(612,792),1.25,dpr);assert.equal(m.width,Math.floor(612*1.25*dpr));assert.equal(m.height,Math.floor(792*1.25*dpr));assert.equal(m.limited,false);}
for(const size of [0,-1,NaN,Infinity]){assert.throws(()=>metrics(page(size,200),1,1),/geometry/);assert.throws(()=>metrics(page(200,size),1,1),/geometry/);assert.throws(()=>metrics(page(200,200),size,1),/geometry/);}
for(const transform of [null,[],[1,0,0,1,0,Infinity]])assert.throws(()=>metrics({getViewport(){return {width:200,height:200,transform};}},1,1),/geometry/);
assert(metrics(page(200,200000),1,2).limited);assert(metrics(page(200,200),1,100).limited);
console.log(JSON.stringify({status:'PASS',boundedGeometryCases:count,normalDprControls:2,invalidCases:15}));
