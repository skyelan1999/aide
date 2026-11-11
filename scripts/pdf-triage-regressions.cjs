'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const worker='internal/server/web/vendor/pdfjs/pdf.worker.min.mjs';
async function load(source,extra=''){return import('data:text/javascript;base64,'+Buffer.from(source+extra).toString('base64'));}
async function main(){
 const source=fs.readFileSync(worker,'utf8');
 const {oi,JpxImage,RunLengthStream,Stream}=await load(source,';export {oi,JpxImage,RunLengthStream,Stream};');
 const bytes=new Uint8Array(Buffer.from(JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-components.json','utf8')).images[0].base64,'base64'));
 const module=oi({warn:()=>{}}),originalMalloc=module._malloc;
 let writes=0,allocations=0;
 const write=module.writeArrayToMemory;
 module.writeArrayToMemory=(...args)=>{writes++;return write(...args);};
 module._malloc=()=>{allocations++;return 0;};
 assert.throws(()=>module.decode(bytes,{numComponents:3}),/input allocation failed/);
 assert.equal(writes,0,'failed input allocation must not copy to offset zero');
 allocations=0;
 for(let n=0;n<12;n++)assert.throws(()=>module.decode(bytes.subarray(0,n),{numComponents:3}),/input too short/);
 assert.equal(allocations,0,'short signatures must fail before entering the native module');
 module._malloc=originalMalloc;
 assert.equal(module.decode(bytes,{numComponents:3}).length,48);
 assert.equal(JpxImage.decode(bytes,{numComponents:3}).length,48);
 // Inject an unexpected imported-callback failure into the real decoder factory;
 // assert a subsequent valid decode creates a new module and preserves pixels.
 const needle='this.#y||=Ii({warn});';assert.equal(source.split(needle).length,2);
 const hooked=source.replace(needle,'this.#y||=triageFactory({warn});');
 const api=await load(hooked,`;let triageFail=false,triageInstances=0;function triageFactory(options){triageInstances++;const m=Ii(options),decode=m.decode;m.decode=(...args)=>{if(triageFail){triageFail=false;throw new Error('synthetic imported callback failure');}return decode(...args);};return m;}export {JpxImage};export function failNext(){triageFail=true;}export function instances(){return triageInstances;}`);
 const expected=api.JpxImage.decode(bytes,{numComponents:3});assert.equal(api.instances(),1);
 api.failNext();assert.throws(()=>api.JpxImage.decode(bytes,{numComponents:3}),/synthetic imported callback failure/);
 assert.deepEqual(api.JpxImage.decode(bytes,{numComponents:3}),expected);assert.equal(api.instances(),2);
 const limit=64*1024*1024;
 for(const encoded of [[129,42],[0,42]]){
  const stream=new RunLengthStream(new Stream(new Uint8Array(encoded)));
  stream.bufferLength=limit;stream.ensureBuffer=()=>{throw Error('allocation trap');};
  assert.throws(()=>stream.readBlock(),/RunLengthDecode output exceeds 64 MiB/);
 }
 assert.throws(()=>new RunLengthStream(new Stream(new Uint8Array([128])),limit+1),/64 MiB/);
 const normal=new RunLengthStream(new Stream(new Uint8Array([2,1,2,3,254,7,128])));
 assert.deepEqual([...normal.getBytes()],[1,2,3,7,7,7]);
 const boundary=new RunLengthStream(new Stream(new Uint8Array([129,7])));
 boundary.bufferLength=limit-128;let requested;
 boundary.ensureBuffer=n=>(requested=n,{});boundary.readBlock();assert.equal(requested,limit);assert.equal(boundary.bufferLength,limit);
 console.log('PASS PDF triage: allocation-failure no-copy, 12 short-input guards, cached-module recovery, RunLength bounds/exact boundary and normal control');
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
