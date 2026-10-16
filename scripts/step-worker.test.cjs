'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/step-import-worker.js','utf8');
const mesh=()=>({attributes:{position:{array:[0,0,0,1,0,0,0,1,0]},normal:{array:[0,0,1,0,0,1,0,0,1]}},index:{array:[0,1,2]},color:[1,.5,0],brep_faces:[{first:0,last:0,color:[.1,.2,.3]}]});
function harness(result,fetch) {
 let batches=0,unacked=false,options;
 const messages=[];
 const self={location:{origin:'https://aide.test'},occtimportjs:async()=>({ReadFile:(type,bytes,settings)=>{options=settings;return result;}}),postMessage(message,transfers){
  messages.push(message);
  if(message.kind==='meshes') {
   assert.equal(unacked,false,'worker must wait for main-thread acknowledgement');unacked=true;batches++;
   assert.ok(transfers.length);assert.equal(new Set(transfers).size,transfers.length);
   setTimeout(()=>{unacked=false;self.onmessage({data:{kind:'ack',id:message.id}});},2);
  }
 }};
 vm.runInNewContext(source,{self,importScripts(){},fetch,URL,TextDecoder,atob,Uint8Array,Float32Array,Uint32Array,Date,Promise,Error,Array,Object,Number,String,Math});
 return {self,messages,get options(){return options;},get batches(){return batches;}};
}
(async()=>{
 const small=harness({success:true,meshes:[mesh(),mesh()]});await small.self.onmessage({data:{buffer:new ArrayBuffer(32)}});
 assert.equal(small.messages.at(-1).kind,'complete');assert.equal(small.messages.at(-1).triangles,2);assert.equal(small.batches,1);
 const large=harness({success:true,meshes:Array.from({length:129},mesh)});await large.self.onmessage({data:{buffer:new ArrayBuffer(64*1024*1024+1)}});
 assert.equal(large.batches,2);assert.equal(large.options.linearDeflection,.5);
 const batches=large.messages.filter(m=>m.kind==='meshes');assert.equal(batches[0].meshes[0].sourceCount,128);assert.equal(batches[0].meshes[0].attributes.position.array.length,128*9);
 assert.ok(Math.abs(batches[0].meshes[0].attributes.color.array[0]-.1)<1e-6,'face colour retained when merged');
 let cancelled=false;
 const over=harness(null,async()=>({ok:true,headers:{get:()=>String(385*1024*1024)},body:{cancel:async()=>{cancelled=true;}}}));
 await over.self.onmessage({data:{url:'https://aide.test/api/file/raw?path=large.step'}});assert.equal(cancelled,true);assert.match(over.messages.at(-1).error,/384/);
 const foreign=harness(null,()=>{throw new Error('must not fetch');});await foreign.self.onmessage({data:{url:'https://evil.test/model.step'}});assert.match(foreign.messages.at(-1).error,/当前工作台/);
 const broken=harness(null,async()=>({ok:true,headers:{get:()=>10},body:{getReader:()=>({read:async()=>({done:true}),releaseLock(){}})}}));
 await broken.self.onmessage({data:{url:'/api/file/raw'}});assert.match(broken.messages.at(-1).error,/传输未完成/);
 const empty=harness({success:true,meshes:[{attributes:{position:{array:[]}},index:{array:[]}}]});await empty.self.onmessage({data:{buffer:new ArrayBuffer(1)}});assert.match(empty.messages.at(-1).error,/没有可显示/);
 const memory=harness({get success(){throw new Error('out of memory');}});await memory.self.onmessage({data:{buffer:new ArrayBuffer(1)}});assert.match(memory.messages.at(-1).error,/内存不足/);
 const encode=a=>Buffer.from(new Float32Array(a).buffer).toString('base64');
 const wire=[{kind:'native-mesh',positions:encode([0,0,0,1,0,0,0,1,0]),normals:encode([0,0,1,0,0,1,0,0,1]),colors:encode([1,0,0,1,0,0,1,0,0]),origin:[100,200,300],sourceCount:1},{kind:'complete',unit:'mm',triangles:1}].map(x=>JSON.stringify(x)+'\n').join('');
 let sent=false;
 const native=harness(null,async(url,options)=>{assert.equal(new URL(url).pathname,'/api/file/step-preview');assert.equal(new URL(url).searchParams.has('access_token'),false);assert.equal(options.headers.Authorization,'Bearer fixture-token');return {ok:true,status:200,body:{getReader:()=>({read:async()=>sent?{done:true}:(sent=true,{value:Buffer.from(wire),done:false}),cancel:async()=>{},releaseLock(){}})}};});
 await native.self.onmessage({data:{url:'/api/file/raw?path=large.step&access_token=fixture-token',native:true}});
 assert.equal(native.messages.at(-1).kind,'complete');assert.deepEqual(Array.from(native.messages.find(x=>x.kind==='meshes').meshes[0].origin),[100,200,300]);assert.equal(native.batches,1);
 let compatibilityCalls=0,compatibilityCancelled=false;
 const compatibility=harness(null,async()=>++compatibilityCalls===1?{status:501,body:{cancel:async()=>{}}}:{ok:true,headers:{get:()=>String(65*1024*1024)},body:{cancel:async()=>{compatibilityCancelled=true;}}});
 await compatibility.self.onmessage({data:{url:'/api/file/raw?path=large.step&access_token=fixture',native:true}});
 assert.equal(compatibilityCalls,2);assert.equal(compatibilityCancelled,true);assert.match(compatibility.messages.at(-1).error,/64 MiB/);
 // Closing/replacing any viewer must remove global handlers that retain meshes.
 const app=fs.readFileSync('internal/server/web/app.js','utf8'),start=app.indexOf('function bindViewerTeardown('),end=app.indexOf('\nasync function setupStlPreview',start);
 const handlers=new Map(),dialog={addEventListener:(k,v)=>handlers.set('dialog:'+k,v),removeEventListener:k=>handlers.delete('dialog:'+k)};
 const context={window:{addEventListener:(k,v)=>handlers.set(k,v),removeEventListener:k=>handlers.delete(k)}};vm.createContext(context);vm.runInContext(app.slice(start,end),context);
 let released=0;const container={closest:()=>dialog};context.bindViewerTeardown(container,()=>released++);assert.equal(handlers.size,2);const release=container._aideViewerTeardown;release();release();assert.equal(released,1);assert.equal(handlers.size,0);assert.equal(container._aideViewerTeardown,null);
 console.log('STEP worker: bounded download, acknowledgement backpressure, merged colours, truncation/origin errors, memory errors and viewer teardown pass');
})().catch(error=>{console.error(error);process.exitCode=1;});
