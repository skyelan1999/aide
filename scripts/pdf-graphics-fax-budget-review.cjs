'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
async function main(){
 const worker=fs.readFileSync(process.env.AIDE_PDF_GRAPHICS_WORKER_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8'),api=fs.readFileSync(process.env.AIDE_PDF_GRAPHICS_API_BASELINE||'internal/server/web/vendor/pdfjs/pdf.min.mjs','utf8');
 const {StateManager,EvalState,TextState,CCITTFaxDecoder}=await import('data:text/javascript;base64,'+Buffer.from(worker+'\nexport{StateManager,EvalState,TextState,CCITTFaxDecoder};').toString('base64'));
 const {CanvasGraphics,CanvasExtraState}=await import('data:text/javascript;base64,'+Buffer.from(api+'\nexport{CanvasGraphics,CanvasExtraState};').toString('base64'));
 const format=e=>e.name==='FormatError',rows=[];
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 await test('532 evaluator stack256 boundary rejects without mutation and restores',()=>{
  const manager=new StateManager();for(let i=0;i<256;i++)manager.save();const before=manager.state;assert.throws(()=>manager.save(),format);assert.equal(manager.stateStack.length,256);assert.equal(manager.state,before);for(let i=0;i<256;i++)manager.restore();assert.equal(manager.stateStack.length,0);manager.save();manager.restore();
 });
 await test('532 actual EvalState/TextState/CanvasExtraState clone ancestry bounded across nested owners',()=>{
  for(const Type of [EvalState,TextState,CanvasExtraState]){let state=Type===CanvasExtraState?new Type(100,100):new Type();for(let i=0;i<256;i++)state=state.clone();assert.throws(()=>state.clone(),format);}
  let state=new EvalState();for(let i=0;i<256;i++){const manager=new StateManager(state);manager.save();state=manager.state;}assert.throws(()=>new StateManager(state).save(),format);
 });
 await test('532 renderer rejects before canvas/stack mutation and supports normal restore',()=>{
  let saved=0,restored=0;const graphics={ctx:{save(){saved++;},restore(){restored++;}},current:new CanvasExtraState(100,100),stateStack:[],inSMaskMode:false};
  for(let i=0;i<256;i++)CanvasGraphics.prototype.save.call(graphics);assert.throws(()=>CanvasGraphics.prototype.save.call(graphics),format);assert.equal(saved,256);assert.equal(graphics.stateStack.length,256);
  // Restore the actual state/context pairing without requiring unrelated renderer fields.
  while(graphics.stateStack.length){graphics.current=graphics.stateStack.pop();graphics.ctx.restore();}assert.equal(restored,256);CanvasGraphics.prototype.save.call(graphics);assert.equal(saved,257);
 });
 await test('533 invalid columns/rows reject before scratch allocation or source read',()=>{
  const Native=globalThis.Uint32Array;let allocations=0,reads=0;
  globalThis.Uint32Array=new Proxy(Native,{construct(target,args){allocations++;throw Error('owned fax allocation trap');}});
  try{for(const options of [{Columns:10000000},{Columns:0},{Columns:-1},{Columns:1.5},{Columns:Infinity},{Columns:65537},{Columns:1728,Rows:-1},{Columns:1728,Rows:0.5},{Columns:65536,Rows:257}])assert.throws(()=>new CCITTFaxDecoder({next(){reads++;return -1;}},options),format);assert.equal(allocations,0);assert.equal(reads,0);}finally{globalThis.Uint32Array=Native;}
 });
 await test('533 default/65536-column scratch boundary and ordinary white row',()=>{
  for(const options of [{},{Columns:65536,Rows:256},{Columns:8,Rows:1}]){const decoder=new CCITTFaxDecoder({next:()=>-1},options);assert.equal(decoder.codingLine.length,(options.Columns||1728)+1);assert.equal(decoder.refLine.length,(options.Columns||1728)+2);}
  const bytes=[0x98,0,0],source={next:()=>bytes.length?bytes.shift():-1},decoder=new CCITTFaxDecoder(source,{Columns:8,Rows:1,EndOfBlock:false});assert.equal(decoder.readNextChar(),255);assert.equal(decoder.readNextChar(),-1);
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
