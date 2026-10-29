'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),acorn=require('../internal/server/code_analysis/acorn.cjs');
async function main(){
 let source=fs.readFileSync(process.env.AIDE_PDF_FONT_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 if(process.env.AIDE_PDF_FONT_BASELINE){
  // Baseline-only watchdog before each width assignment: never allocate the reported huge range.
  source=source.replace('for(let e=t;e<=i;e++)s[e]=r','for(let e=t;e<=i;e++){if(++globalThis.ownedWidthWrites>globalThis.ownedWidthLimit)throw Error("owned width trap");s[e]=r}').replace('for(let e=t;e<=s;e++)n[e]=r','for(let e=t;e<=s;e++){if(++globalThis.ownedWidthWrites>globalThis.ownedWidthLimit)throw Error("owned width trap");n[e]=r}');
 }
 const {PartialEvaluator,Dict,CMap,FormatError}=await import('data:text/javascript;base64,'+Buffer.from(source+'\nexport {PartialEvaluator,Dict,CMap,FormatError};').toString('base64'));
 const rows=[],format=e=>e.name==='FormatError';
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 const widths=(entries,vertical=false)=>{const d=new Dict();for(const[k,v]of Object.entries(entries))d.set(k,v);const output={composite:true,vertical,flags:0};PartialEvaluator.prototype.extractWidths.call({xref:{fetchIfRef:x=>x}},d,new Dict(),output);return output;};
 await test('512 invalid CID range/array dimensions reject before writes',()=>{
  globalThis.ownedWidthWrites=0;globalThis.ownedWidthLimit=256;
  for(const entries of [{W:[0,10000000,500]},{W2:[0,10000000,-1000,250,880]},{W:[-1,2,500]},{W:[65536,[500]]},{W:[65535,[500,500]]},{W:[2,1,500]},{W2:[0,[1,2]]},{W2:[65535,[1,2,3,4,5,6]]}])assert.throws(()=>widths(entries,true),format);
  assert.equal(globalThis.ownedWidthWrites,0);
 });
 await test('512 full16bit CID horizontal/vertical endpoints and exact aggregate limit',()=>{
  globalThis.ownedWidthWrites=0;globalThis.ownedWidthLimit=Infinity;
  const result=widths({W:[0,65535,500],W2:[0,65535,-1000,250,880]},true);assert.equal(result.widths.length,65536);assert.equal(result.widths[65535],500);assert.deepEqual(result.vmetrics[65535],[-1000,250,880]);
  assert.throws(()=>widths({W:[0,65535,500,0,65535,500,0,[500]]}),format);
  const ordinary=widths({W:[1,[120,240],5,7,480],W2:[1,[-900,60,820,-950,120,840]]},true);assert.equal(ordinary.widths[2],240);assert.equal(ordinary.widths[7],480);assert.deepEqual(ordinary.vmetrics[2],[-950,120,840]);
 });
 await test('514 huge/invalid CMap spans reject before first assignment',()=>{
  for(const method of ['mapCidRange','mapBfRange','mapBfRangeToArray'])for(const [start,end] of [[0,0xffffff],[-1,2],[2,1],[0,Infinity],[0,4294967296]]){
   const cmap=new CMap();let writes=0;cmap._map=new Proxy([],{set(target,key,value){if(++writes>256)throw Error('owned CMap write trap');return Reflect.set(target,key,value);}});
   assert.throws(()=>cmap[method](start,end,method==='mapCidRange'?0:method==='mapBfRange'?'\0':['\0']),format);assert.equal(writes,0);
  }
 });
 await test('514 inclusive131072 limit, cumulative overwrites/mapOne and normal mappings',()=>{
  const cmap=new CMap();cmap.mapCidRange(0,65535,0);cmap.mapCidRange(65536,131071,10);assert.equal(cmap.lookup(65535),65535);assert.equal(cmap.lookup(131071),65545);assert.throws(()=>cmap.mapOne(0,1),format);
  const duplicates=new CMap();duplicates.mapCidRange(0,65535,0);duplicates.mapCidRange(0,65535,1);assert.throws(()=>duplicates.mapOne(5,2),format);
  const normal=new CMap();normal.mapBfRange(2,4,'\0A');normal.mapBfRangeToArray(6,7,['\0Z','\0Y']);normal.mapOne(8,'\0X');assert.equal(normal.lookup(4),'\0C');assert.equal(normal.lookup(7),'\0Y');assert.equal(normal.lookup(8),'\0X');
 });
 // Extract the real appearance callback; its only mutable capture is the point allowance.
 const ast=acorn.parse(source,{ecmaVersion:'latest',sourceType:'module'}),cls=ast.body.find(n=>n.type==='ClassDeclaration'&&n.id.name==='SquigglyAnnotation');let callback;
 function walk(n){if(!n||typeof n!=='object')return;if(n.type==='Property'&&n.key.name==='pointsCallback')callback=n.value;for(const v of Object.values(n))if(Array.isArray(v))v.forEach(walk);else if(v&&typeof v==='object')walk(v);}
 walk(cls);assert(callback);const makeCallback=()=>new Function('FormatError','let remaining=65536;return ('+source.slice(callback.start,callback.end)+');')(FormatError);
 await test('513 huge/nonfinite coordinates stop before path generation',()=>{
  for(const quad of [[1e20,10,2e20,10,1e20,0,2e20,0],[0,10,1e8,10,0,0,1e8,0],[0,Infinity,10,Infinity,0,0,10,0]]){const points=[];if(process.env.AIDE_PDF_FONT_BASELINE)points.push=function(...values){if(this.length>256)throw Error('owned annotation point trap');return Array.prototype.push.apply(this,values);};assert.throws(()=>makeCallback()(points,new Float32Array(quad)),format);assert.equal(points.length,0);}
 });
 await test('513 ordinary wave geometry and aggregate quad point budget',()=>{
  const points=[],bounds=makeCallback()(points,new Float32Array([0,12,8,12,0,0,8,0]));assert.equal(points.length,6);assert.equal(points[0],'0 2 m');assert.equal(points[1],'2 0 l');assert.equal(points[4],'8 2 l');assert.equal(points[5],'S');assert.deepEqual(bounds,[0,8,-4,4]);
  const callback=makeCallback();callback([],new Float32Array([0,12,131072,12,0,0,131072,0]));assert.throws(()=>callback([],new Float32Array([0,12,2,12,0,0,2,0])),format);
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
