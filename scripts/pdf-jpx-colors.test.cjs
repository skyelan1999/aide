'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),{convertJPXColors}=require('./pdf-jpx-colors.cjs');
async function main(){
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {oi}=await import('data:text/javascript;base64,'+Buffer.from(source+';export {oi};').toString('base64')),module=oi({warn:()=>{}});
 const fixtures=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-color-issues.json'));
 const report=[];
 for(const fixture of fixtures){
  const input=Buffer.from(fixture.base64,'base64'),rgba=module.decode(input,{numComponents:0,smaskInData:true});
  assert.equal(typeof rgba,'object',String(rgba));
  if(fixture.number===681)for(let i=0;i<rgba.length;i+=4)assert.deepEqual([...rgba.slice(i,i+4)],[255,0,255,255]);
  if(fixture.number===689)for(let i=0;i<rgba.length;i+=4)assert.deepEqual([...rgba.slice(i,i+4)],[255,124,255,255]);
  if(fixture.number===690){assert.deepEqual([...rgba.slice(8,12)],[62,62,62,255]);assert.deepEqual([...rgba.slice(12,16)],[0,0,0,255]);}
  if(fixture.number===692)assert.deepEqual([...rgba.slice(0,4)],[255,255,255,128]);
  if(fixture.number===700){
   const raw=module.decode(input,{numComponents:4,smaskInData:true}),opaque=module.decode(input,{numComponents:0,smaskInData:false});
   for(let p=0;p<rgba.length;p+=4){assert.deepEqual([...rgba.slice(p,p+3)],[...opaque.slice(p,p+3)]);assert.equal(rgba[p+3],raw[p+3]);assert.equal(opaque[p+3],255);}
   assert.notDeepEqual([...rgba.slice(4,7)],[...raw.slice(4,7)]);
  }
  report.push({issue:fixture.number,length:rgba.length,first:[...rgba.slice(0,16)]});
 }
 // Independent reference-grid edge test: absent odd-origin chroma is neutral,
 // while the next available sample uses its actual non-neutral chroma.
 const meta={width:2,height:2,x0:1,y0:1,components:3,color:18,specs:[{dx:1,dy:1},{dx:2,dy:2},{dx:2,dy:2}]};
 const edge=convertJPXColors(new Uint8ClampedArray([128,0,0,128,0,0,128,0,0,128,0,0]),meta,false);
 for(const p of [0,4,8])assert.deepEqual([...edge.slice(p,p+4)],[128,128,128,255]);assert.notDeepEqual([...edge.slice(12,16)],[128,128,128,255]);
 console.log(JSON.stringify({status:'PASS',rows:report,oddOriginNeutralEdge:true}));
}
main().catch(e=>{console.error(e);process.exitCode=1;});
