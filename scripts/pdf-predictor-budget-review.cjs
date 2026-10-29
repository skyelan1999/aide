'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
async function runtime(file=process.env.AIDE_PDF_WORKER_REVIEW||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs') {
 const source=fs.readFileSync(file,'utf8');
 return import('data:text/javascript;base64,'+Buffer.from(source+'\nexport {PredictorStream,Dict,DecodeStream};').toString('base64'));
}
async function main(){
 const {PredictorStream,Dict,DecodeStream}=await runtime(),rows=[];
 const params=values=>{const d=new Dict();for(const [k,v] of Object.entries(values))d.set(k,v);return d;};
 function stream(bytes=[]){let pos=0;const reads=[];return{dict:new Dict(),reads,getBytes(n){reads.push(n);assert(n<=8*1024*1024,'owned source allocation trap');const out=Uint8Array.from(bytes.slice(pos,pos+n));pos+=out.length;return out;},getByte(){return pos<bytes.length?bytes[pos++]:-1;}};}
 function test(name,fn){try{rows.push({name,status:'PASS',detail:fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message})}}
 test('reject invalid DecodeParms before decoded reads',()=>{
  const cases=[{Columns:33554432,Colors:4},{Columns:1048576,Colors:4096,BitsPerComponent:1},{Columns:8*1024*1024+1},{Colors:0},{Colors:-1},{Columns:0},{Columns:1.5},{Columns:Number.MAX_SAFE_INTEGER},{BitsPerComponent:3},{BitsPerComponent:0},{Predictor:11.5},{Predictor:-1},{Colors:'3'},{Predictor:'12'}];
  for(const item of cases){const s=stream();let error;try{new PredictorStream(s,0,params({Predictor:12,...item}));}catch(e){error=e}assert.equal(error?.name,'FormatError',JSON.stringify(item));assert.equal(s.reads.length,0);}
  assert.equal(new PredictorStream(stream(),0,params({Predictor:1})).constructor.name,'Object');return{cases:cases.length};
 });
 test('exact row cap, clean EOF and truncated rows allocate no output',()=>{
  const saved=DecodeStream.prototype.ensureBuffer;let allocations=0;
  DecodeStream.prototype.ensureBuffer=function(){allocations++;throw Error('owned output allocation trap');};
  try{
   for(const predictor of [2,12]){
    const clean=new PredictorStream(stream(),0,params({Predictor:predictor,Columns:8*1024*1024}));assert.equal(clean.rowBytes,8*1024*1024);clean.readBlock();assert.equal(clean.eof,true);
    const short=new PredictorStream(stream(predictor===2?[1,2]:[0,1,2]),0,params({Predictor:predictor,Columns:3}));assert.throws(()=>short.readBlock(),e=>e.name==='FormatError');
   }
   assert.equal(allocations,0);
  }finally{DecodeStream.prototype.ensureBuffer=saved;}
 });
 test('TIFF normal byte, word and packed-sample controls',()=>{
  for(const [bits,input,expected,columns] of [[8,[1,1,1],[1,2,3],3],[16,[0,1,0,1,0,1],[0,1,0,2,0,3],3],[4,[0x11],[0x12],2],[2,[0x55],[0x6c],4],[1,[0x55],[0x66],8]]){
   const decoder=new PredictorStream(stream(input),0,params({Predictor:2,Columns:columns,BitsPerComponent:bits}));assert.deepEqual(Array.from(decoder.getBytes()),expected);
  }
 });
 test('PNG filters0..4 and multichannel two-row controls',()=>{
  const paeth=(a,b,c)=>{const p=a+b-c,pa=Math.abs(p-a),pb=Math.abs(p-b),pc=Math.abs(p-c);return pa<=pb&&pa<=pc?a:pb<=pc?b:c};
  for(const colors of [1,3,4])for(let filter=0;filter<=4;filter++){
   const width=4*colors,previous=Array(width).fill(0),expected=[],encoded=[];
   for(let row=0;row<2;row++){
    const data=Array.from({length:width},(_,i)=>(i*31+row*17)%256);encoded.push(filter);
    for(let i=0;i<width;i++){const a=i>=colors?data[i-colors]:0,b=previous[i],c=i>=colors?previous[i-colors]:0;const predictor=[0,a,b,Math.floor((a+b)/2),paeth(a,b,c)][filter];encoded.push((data[i]-predictor+256)%256);}
    expected.push(...data);previous.splice(0,width,...data);
   }
   const decoder=new PredictorStream(stream(encoded),0,params({Predictor:15,Columns:4,Colors:colors}));assert.deepEqual(Array.from(decoder.getBytes()),expected);
  }
  return{controls:15};
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module)main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
module.exports={runtime};
