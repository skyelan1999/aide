'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),zlib=require('node:zlib');
async function main(){
 const source=fs.readFileSync(process.env.AIDE_PDF_FLATE_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {FlateStream,Stream}=await import('data:text/javascript;base64,'+Buffer.from(source+'\nexport{FlateStream,Stream};').toString('base64'));
 const make=()=>new FlateStream(new Stream(zlib.deflateSync(Buffer.from([255,0,0]))));
 const format=e=>e.name==='FormatError',rows=[];
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 async function mockDecoder(chunks,fn){const Native=globalThis.DecompressionStream,record={pulls:0,cancelled:false};globalThis.DecompressionStream=class{constructor(){this.writable=new WritableStream();this.readable=new ReadableStream({pull(controller){record.pulls++;if(chunks.length)controller.enqueue(chunks.shift());else controller.close();},cancel(){record.cancelled=true;}},{highWaterMark:0});}};try{return await fn(record);}finally{globalThis.DecompressionStream=Native;}}
 await test('534 requested prefix cancels before reading trailing chunks',()=>mockDecoder([new Uint8Array([255,0,0,7,8]),new Uint8Array(1024)],async record=>{assert.deepEqual([...await make().getImageData(3)],[255,0,0]);assert.equal(record.pulls,1);assert.equal(record.cancelled,true);}));
 await test('534 full-output budget rejects before concatenation and never falls back',()=>mockDecoder([{byteLength:67108864},{byteLength:1}],async record=>{const stream=make();stream.getBytes=()=>{throw Error('owned unwanted fallback');};const Native=globalThis.Uint8Array;globalThis.Uint8Array=new Proxy(Native,{construct(target,args){if(typeof args[0]==='number'&&args[0]>1048576)throw Error('owned Flate concatenation trap');return Reflect.construct(target,args);}});try{await assert.rejects(stream.getImageData(),format);assert.equal(record.cancelled,true);}finally{globalThis.Uint8Array=Native;}}));
 await test('534 invalid requested size rejects before resetting or decoding source',async()=>{for(const length of [-1,1.5,Infinity,67108865]){const stream=make();stream.str.reset=()=>{throw Error('owned source reset trap');};await assert.rejects(stream.getImageData(length),format);}});
 await test('534 manual buffer and constructor hints reject before allocation',()=>{
  const Native=globalThis.Uint8Array;let attempts=0;globalThis.Uint8Array=new Proxy(Native,{construct(target,args){if(typeof args[0]==='number'&&args[0]>67108864){attempts++;throw Error('owned Flate allocation trap');}return Reflect.construct(target,args);}});
  try{assert.throws(()=>make().ensureBuffer(67108865),format);assert.equal(attempts,0);assert.throws(()=>new FlateStream(new Stream(zlib.deflateSync(Buffer.from('ok'))),67108865),format);}finally{globalThis.Uint8Array=Native;}
 });
 await test('534 native real compressed prefix/EOF/zero and manual fallback controls',async()=>{
  const raw=Buffer.alloc(1024*1024,7);raw.set([255,0,0]);const encoded=zlib.deflateSync(raw);assert.deepEqual([...await new FlateStream(new Stream(encoded)).getImageData(3)],[255,0,0]);assert.deepEqual([...await make().getImageData(8)],[255,0,0]);assert.equal((await make().getImageData(0)).length,0);
  const Native=globalThis.DecompressionStream;try{globalThis.DecompressionStream=undefined;assert.deepEqual([...await make().getImageData(3)],[255,0,0]);}finally{globalThis.DecompressionStream=Native;}
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
