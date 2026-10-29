'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
async function main(){
 const source=fs.readFileSync(process.env.AIDE_PDF_RESOURCE_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {PartialEvaluator,Dict}=await import('data:text/javascript;base64,'+Buffer.from(source+'\nexport {PartialEvaluator,Dict};').toString('base64'));
 const dict=()=>{const d=new Dict();for(const[k,v]of Object.entries({BBox:[0,0,2,2],XStep:2,YStep:2,PaintType:1,TilingType:1}))d.set(k,v);return d;};
 const owner=()=>Object.assign(Object.create(PartialEvaluator.prototype),{options:{ignoreErrors:false},xref:{fetchIfRef:x=>x}});
 const ops=()=>({count:0,addOp(){this.count++;},addDependencies(){}});
 const rows=[];
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 const enter=(o,kind,d,task,op)=>kind==='form'?o.buildFormXObject(new Dict(),{dict:d},null,op,task,null,null):o.handleTilingType(55,null,new Dict(),{dict:d},d,op,task,{set(){}});
 for(const kind of ['form','pattern']){
  await test(kind+' self/mutual cycles reject before owned recursion trap and recover',async()=>{
   for(const mutual of [false,true]){
    const o=owner(),task={},a=dict(),b=dict(),op=ops();let calls=0;
    o.getOperatorList=async({stream})=>{if(++calls>512)throw Error('owned recursion trap');return enter(o,kind,mutual&&stream.dict===a?b:a,task,op);};
    await assert.rejects(enter(o,kind,a,task,op),e=>e.name==='FormatError'&&/Recursive PDF resource/.test(e.message));
    assert(calls<=2);assert(op.count<=4);o.getOperatorList=async()=>{};await enter(o,kind,a,task,op);
   }
  });
  await test(kind+' depth128 boundary and cleanup after failure',async()=>{
   for(const depth of [128,129]){
    const o=owner(),task={},ds=Array.from({length:depth},dict),op=ops();let calls=0;
    o.getOperatorList=async({stream})=>{calls++;const i=ds.indexOf(stream.dict);if(i+1<ds.length)return enter(o,kind,ds[i+1],task,op);};
    if(depth===128)await enter(o,kind,ds[0],task,op);else await assert.rejects(enter(o,kind,ds[0],task,op),e=>e.name==='FormatError'&&/depth limit/.test(e.message));
    assert.equal(calls,128);o.getOperatorList=async()=>{};await enter(o,kind,ds[0],task,op);
   }
  });
 }
 await test('cross-kind cycle is detected, repeated siblings remain valid',async()=>{
  const o=owner(),task={},a=dict(),b=dict(),op=ops();let calls=0;o.getOperatorList=async({stream})=>{if(++calls>512)throw Error('owned recursion trap');return enter(o,stream.dict===a?'pattern':'form',stream.dict===a?b:a,task,op);};
  await assert.rejects(enter(o,'form',a,task,op),e=>e.name==='FormatError'&&/Recursive PDF resource/.test(e.message));
  o.getOperatorList=async()=>{};for(let i=0;i<50;i++)await enter(o,i%2?'form':'pattern',a,task,op);
 });
 await test('independent concurrent tasks do not share active resources',async()=>{
  const o=owner(),d=dict(),op=ops();let started=0,release;const barrier=new Promise(r=>release=r);
  o.getOperatorList=async()=>{started++;if(started===8)release();await barrier;};
  await Promise.all(Array.from({length:8},(_,i)=>enter(o,i%2?'form':'pattern',d,{},op)));assert.equal(started,8);
 });
 await test('thrown or aborted child releases active resource lease',async()=>{
  const o=owner(),task={},d=dict(),op=ops();for(const kind of ['form','pattern']){o.getOperatorList=async()=>{throw Error('owned cancellation');};await assert.rejects(enter(o,kind,d,task,op),/owned cancellation/);o.getOperatorList=async()=>{};await enter(o,kind,d,task,op);}
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
