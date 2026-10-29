'use strict';
// Exercise the actual extraction entrypoint with shipped JSZip and finite,
// synthetic archives. Rendering/conversion is deliberately stopped after parse.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const JSZip=require('../internal/server/web/vendor/drawio/js/jszip/jszip.min.js');
const base='internal/server/web/vendor/drawio/js/';
const files=['diagramly/vsdx/importer.js','extensions.min.js','integrate.min.js'];
function methods(file){
 const text=fs.readFileSync(base+file,'utf8');
 if(file===files[0]){
  const a=text.indexOf('                /* AIDE Visio archive budgets #176 */');
  const start=text.indexOf('mxVsdxCodec.prototype.decodeVsdx = function');
  const end=text.indexOf('mxVsdxCodec.prototype.createMxGraph',start);
  assert.ok(start>=0&&end>start);
  return text.slice(a>=0?a:start,end);
 }
 const marker='/* AIDE Visio archive budgets #176 */';
 const a=text.indexOf(marker);const start=a>=0?a:text.indexOf('m.prototype.decodeVsdx=function');
 const end=text.indexOf('m.prototype.createMxGraph=',start);assert.ok(start>=0&&end>start);
 return text.slice(start,end);
}
function stencilMethod(file){
 const text=fs.readFileSync(base+file,'utf8');
 const a=text.indexOf('/* AIDE Visio stencil error forwarding #176 */');
 const b=text.indexOf(file===files[0]?'mxVssxCodec.prototype.normalizeGeo':'k.prototype.normalizeGeo=',a);
 assert.ok(a>=0&&b>a);return text.slice(a,b);
}
function runtime(file,zip=JSZip,extras={}){
 const parsed=[];function Codec(){}
 Codec.vsdxPlaceholder='visio';Codec.incorrectXMLReqExp=[];
 Codec.parseXml=text=>{parsed.push(text.length);return {firstChild:{nodeType:1}};};
 const stop=new Error('owned-stop-after-parse');Codec.prototype.importNodes=()=>{throw stop;};
 const context={mxVsdxCodec:Codec,m:Codec,com:{mxgraph:{io:{mxVsdxCodec:function PreviousCodec(){}}}},JSZip:zip,console:{log(){}},Promise,Uint8Array,ArrayBuffer,Number,Error,Object,...extras};
 vm.runInNewContext(methods(file),context,{timeout:5000});
 assert.equal(typeof Codec.loadArchive,'function','assign the loader before publishing the constructor');
 assert.equal(typeof Codec.prototype.decodeVsdx,'function','assign decode on the local constructor');
 context.com.mxgraph.io.mxVsdxCodec=Codec;
 function StencilCodec(){}StencilCodec.prototype=Object.create(Codec.prototype);
 context.mxVssxCodec=StencilCodec;context.k=StencilCodec;
 context.com.mxgraph.io.mxVssxCodec=function PreviousStencilCodec(){};
 vm.runInNewContext(stencilMethod(file),context,{timeout:5000});
 assert.equal(typeof StencilCodec.prototype.decodeVssx,'function');
 context.com.mxgraph.io.mxVssxCodec=StencilCodec;
 return {Codec,StencilCodec,parsed,stop,context};
}
async function runDecode(r,data,stencil=false){
 return new Promise((resolve,reject)=>{
  const timer=setTimeout(()=>reject(Error('decode timed out')),5000);
  const end=value=>{r.callbackCount=(r.callbackCount||0)+1;clearTimeout(timer);resolve(value);};
  const codec=stencil?new r.StencilCodec():new r.Codec();
  codec[stencil?'decodeVssx':'decodeVsdx'](data,()=>end(Error('unexpected success')),null,end);
 });
}
async function archive(members){
 const zip=new JSZip();for(const [name,data] of members)zip.file(name,data,{createFolders:false});
 return zip.generateAsync({type:'uint8array',compression:'DEFLATE'});
}
function observeZip(mutate){
 const state={active:0,maxActive:0,started:[]};
 class ObservedZip extends JSZip {}
 ObservedZip.loadAsync=async data=>{
  const zip=await JSZip.loadAsync(data);if(mutate)mutate(zip);
  for(const entry of Object.values(zip.files)){
   const original=entry.internalStream;
   entry.internalStream=function(type){
    const stream=original.call(this,type),pause=stream.pause;let active=true;
    state.started.push(this.name);state.maxActive=Math.max(state.maxActive,++state.active);
    function end(){if(active){active=false;state.active--;}}
    stream.on('end',end).on('error',end);stream.pause=function(){end();return pause.call(this);};return stream;
   };
  }
  return zip;
 };
 return {Zip:ObservedZip,state};
}
async function main(){
 const entryLimit=8*1024*1024,totalLimit=16*1024*1024;
 const oversized=await archive([['visio/document.xml','<VisioDocument><!--'+'x'.repeat(entryLimit)+'--></VisioDocument>']]);
 const validMembers=[['visio/document.xml','<VisioDocument>owned</VisioDocument>'],['visio/_rels/document.xml.rels','<Relationships/>'],['visio/media/owned.bin',Uint8Array.from([0,127,128,255])],['visio/media/owned.bmp',Uint8Array.from([66,77,0,255])]];
 const valid=await archive(validMembers);
 const boundary=await archive([['visio/media/exact.bin',new Uint8Array(entryLimit)],['visio/media/second.bin',new Uint8Array(entryLimit)]]);
 const tooMany=await archive(Array.from({length:1025},(_,i)=>['ignored-'+i+'.bin','']));
 const aggregate=await archive([['visio/media/a.bin',new Uint8Array(6*1024*1024)],['visio/media/b.bin',new Uint8Array(6*1024*1024)],['visio/media/c.bin',new Uint8Array(6*1024*1024)],['visio/media/not-started.bin','owned']]);
 for(const file of files){
  let r=runtime(file),error=await runDecode(r,oversized);
  assert.equal(error?.code,'AIDE_VISIO_LIMIT',file+' rejects expansion before XML parse');assert.deepEqual(r.parsed,[]);
  console.log('PASS',file,'declared oversized member rejected before parse');
  const stencil=runtime(file);assert.equal((await runDecode(stencil,oversized,true))?.code,'AIDE_VISIO_LIMIT','VSSX forwards archive failures');
  await new Promise(setImmediate);assert.equal(stencil.callbackCount,1);assert.deepEqual(stencil.parsed,[]);
  const observed=observeZip();r=runtime(file,observed.Zip);
  const loaded=await r.Codec.loadArchive(valid);
  for(const [name,data]of validMembers){
   const bytes=await loaded.file(name).async('uint8array');
   assert.deepEqual(Buffer.from(bytes),typeof data==='string'?Buffer.from(data):Buffer.from(data));
  }
  assert.equal(observed.state.maxActive,1,'compressed entries expanded serially');assert.equal(observed.state.active,0);
  const normal=runtime(file);assert.equal(await runDecode(normal,await archive(validMembers.filter(([name])=>!name.endsWith('.bmp')))),normal.stop);assert.equal(normal.parsed.length,2,'valid XML and rels reach the existing converter');
  const exact=await r.Codec.loadArchive(boundary);assert.equal((await exact.file('visio/media/second.bin').async('uint8array')).length,entryLimit);
  await assert.rejects(r.Codec.loadArchive(new Uint8Array(entryLimit+1)),{code:'AIDE_VISIO_LIMIT'});
  await assert.rejects(r.Codec.loadArchive(tooMany),{code:'AIDE_VISIO_LIMIT'});
  await assert.rejects(r.Codec.loadArchive(aggregate),{code:'AIDE_VISIO_LIMIT'});
  const lying=observeZip(zip=>{for(const entry of Object.values(zip.files))entry._data.uncompressedSize=1;});
  r=runtime(file,lying.Zip);error=await runDecode(r,oversized);
  assert.equal(error?.code,'AIDE_VISIO_LIMIT','actual streamed bytes override lying metadata');assert.deepEqual(r.parsed,[]);assert.equal(lying.state.maxActive,1);
  // Hide size metadata without changing JSZip's own integrity check.
  const hidden=observeZip(zip=>{for(const entry of Object.values(zip.files))Object.defineProperty(entry,'_data',{value:new Proxy(entry._data,{get(target,key){if(key==='uncompressedSize')return undefined;const value=Reflect.get(target,key);return typeof value==='function'?value.bind(target):value;}})});});
  r=runtime(file,hidden.Zip);await assert.rejects(r.Codec.loadArchive(aggregate),{code:'AIDE_VISIO_LIMIT'});
  assert.ok(!hidden.state.started.includes('visio/media/not-started.bin'),'no later entry starts after actual aggregate overflow');assert.equal(hidden.state.maxActive,1);
  await assert.rejects(runtime(file).Codec.loadArchive(Uint8Array.from([1,2,3])),/zip|central directory/i);
  console.log('PASS',file,'raw byte preservation, serial expansion, exact 8/16 MiB boundaries, compressed/count/member/aggregate limits, lying metadata and corrupt input');
 }
 assert.equal(totalLimit,2*entryLimit);
}

module.exports={methods,runtime,runDecode,JSZip,files,archive,observeZip};if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
