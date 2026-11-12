'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
async function load(source,extra=''){return import('data:text/javascript;base64,'+Buffer.from(source+extra).toString('base64'));}
async function main(){
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const fixtures=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-allocation-layer.json'));
 const bytes=name=>Buffer.from(fixtures.find(x=>x.name.includes(name)).base64,'base64');
 const {oi}=await load(source,';export {oi};'),normal=oi({warn:()=>{}});
 const control=normal.decode(bytes('one-layer'),{numComponents:1}),late=normal.decode(bytes('layer1000'),{numComponents:1});
 assert.equal(control.length,256);assert.equal(new Set(control).size>1,true);assert.deepEqual(late,control,'first inclusion at layer 1000 changed samples');
 const allocation='const pointer=aideNativeAllocate(a,b)|0;';assert.equal(source.split(allocation).length,2);
 const instrumented=source.replace(allocation,'if(Module.triageRequests)Module.triageRequests.push(b);const pointer=(++Module.triageCount===Module.triageFailAt)?0:aideNativeAllocate(a,b)|0;');
 const api=await load(instrumented,';export {oi};');let failures=0;
 for(const name of ['622-','707-']){
  const input=bytes(name),baseline=api.oi({warn:()=>{}});baseline.triageCount=0;baseline.triageRequests=[];
  assert.equal(typeof baseline.decode(input,{numComponents:0}),'object');
  const count=baseline.triageRequests.length;assert(count>50&&count<400);
  // Exercise every actual allocation in both supplied valid fixtures. This
  // includes both 20-component header arrays and every tag-tree node array.
  for(let n=1;n<=count;n++){
   const module=api.oi({warn:()=>{}});module.triageCount=0;module.triageFailAt=n;
   assert.throws(()=>module.decode(input,{numComponents:0}),/native allocation failed/,'allocation '+n);
   assert.equal(module.aideAllocationFailed,true);
   assert.throws(()=>module.decode(input,{numComponents:0}),/requires recreation/);
   failures++;
  }
 }
 const bounded=oi({warn:()=>{}});bounded.aideAllocationBytes=268435455;
 assert.throws(()=>bounded._malloc(2),/allocation budget exceeded/);
 assert.equal(bounded.aideAllocationFailed,true);
 const factory='this.#y||=Ii({warn});';assert.equal(instrumented.split(factory).length,2);
 const recovery=await load(instrumented.replace(factory,'this.#y||=triageFactory({warn});'),`;let instances=0,nextFailure=0;function triageFactory(o){instances++;const m=Ii(o);m.triageCount=0;m.triageFailAt=nextFailure;nextFailure=0;return m;}export {JpxImage};export function failNext(n){JpxImage.cleanup();nextFailure=n;}export function count(){return instances;}`);
 const fixture=bytes('707-'),expected=recovery.JpxImage.decode(fixture,{numComponents:0});
 recovery.failNext(49);assert.throws(()=>recovery.JpxImage.decode(fixture,{numComponents:0}),/native allocation failed/);
 assert.deepEqual(recovery.JpxImage.decode(fixture,{numComponents:0}),expected);assert.equal(recovery.count(),3);
 console.log(JSON.stringify({status:'PASS',firstInclusionLayer1000:'identical to one-layer control',allocationFailures:failures,poisonedModuleRejected:true,freshModuleRecovered:true}));
}
main().catch(e=>{console.error(e);process.exitCode=1;});
