'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),crypto=require('node:crypto');
const fixtures=JSON.parse(fs.readFileSync(__dirname+'/fixtures/pdf-jpx-alpha.json'));
const mattes=[[0,0,0],[0,0,0],[0,0,0],[0,1,0],[1,0,0],[0,0,1]];
const expected=[[128,0,0],[0,128,0],[0,0,128],[128,128,128],[128,128,0],[128,128,128]];
function makePDF(){
 const chunks=[Buffer.from('%PDF-1.7\n')],offsets=[0];let length=chunks[0].length;
 function object(n,data){offsets[n]=length;const b=Buffer.concat([Buffer.from(n+' 0 obj\n'),Buffer.isBuffer(data)?data:Buffer.from(data),Buffer.from('\nendobj\n')]);chunks.push(b);length+=b.length;}
 object(1,'<< /Type /Catalog /Pages 2 0 R >>');object(2,'<< /Type /Pages /Kids [3 0 R] /Count 1 >>');
 object(3,'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 180 40] /Contents 4 0 R /Resources << /XObject << '+fixtures.images.map((_,i)=>'/I'+i+' '+(i+5)+' 0 R').join(' ')+' >> >> >>');
 const commands='0 0 0 rg 0 0 180 40 re f\n'+fixtures.images.map((_,i)=>`q 20 0 0 20 ${i*30+5} 10 cm /I${i} Do Q`).join('\n');
 object(4,'<< /Length '+Buffer.byteLength(commands)+' >>\nstream\n'+commands+'\nendstream');
 fixtures.images.forEach((image,i)=>{const b=Buffer.from(image.base64,'base64');object(i+5,Buffer.concat([Buffer.from('<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB /SMaskInData 2 /Matte ['+mattes[i].join(' ')+'] /Filter /JPXDecode /Length '+b.length+' >>\nstream\n'),b,Buffer.from('\nendstream')]));});
 const xref=length;chunks.push(Buffer.from('xref\n0 11\n0000000000 65535 f \n'+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+'trailer\n<< /Size 11 /Root 1 0 R >>\nstartxref\n'+xref+'\n%%EOF\n'));return Buffer.concat(chunks);
}
async function main(){
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {PDFImage,JpxStream,Stream,Dict,Name,LocalColorSpaceCache}=await import('data:text/javascript;base64,'+Buffer.from(source+';export {PDFImage,JpxStream,Stream,Dict,Name,LocalColorSpaceCache};').toString('base64'));
 async function decode(fixture,mask,matte,color=true){
  const data=new Uint8Array(Buffer.from(fixture.base64,'base64')),dict=new Dict();
  for(const[k,v]of Object.entries({Filter:Name.get('JPXDecode'),Width:1,Height:1,BitsPerComponent:8}))dict.set(k,v);
  if(mask!==undefined)dict.set('SMaskInData',mask);if(matte)dict.set('Matte',matte);if(color)dict.set('ColorSpace',Name.get('DeviceRGB'));
  const stream=new JpxStream(new Stream(data,0,data.length,dict),data.length,null),image=new PDFImage({xref:{fetchIfRef:x=>x},res:Dict.empty,image:stream,localColorSpaceCache:new LocalColorSpaceCache()});
  assert.equal(image.jpxDecoderOptions.smaskInData,[1,2].includes(mask)?mask:0);
  return (await image.createImageData(true)).data;
 }
 for(let i=0;i<fixtures.images.length;i++){
  const rgba=await decode(fixtures.images[i],2,mattes[i]);assert.equal(rgba.length,4);assert.equal(rgba[3],128);
  const composited=[...rgba.slice(0,3)].map(x=>Math.round(x*rgba[3]/255));assert.deepEqual(composited,expected[i],fixtures.images[i].name);
 }
 assert.deepEqual(await decode(fixtures.images[0],0,null,false),await decode(fixtures.images[0],undefined,null,false));
 assert.notDeepEqual(await decode(fixtures.images[0],1,null,false),await decode(fixtures.images[0],0,null,false));
 // Current CMYK fixture must not mutate low linear memory after the K plane is freed.
 const fixture=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-cmyk-memory.json'));
 const bytes=Buffer.from(fixture.base64,'base64');assert.equal(crypto.createHash('sha256').update(bytes).digest('hex'),fixture.sha256);
 const needle='Module["writeArrayToMemory"]=writeArrayToMemory;';assert.equal(source.split(needle).length,2);
 const instrumented=source.replace(needle,needle+'Module.triageHeap=()=>HEAPU8;');
 const {oi}=await import('data:text/javascript;base64,'+Buffer.from(instrumented+';export {oi};').toString('base64')),module=oi({warn:()=>{}});
 let previous;
 for(let i=0;i<3;i++){const low=module.triageHeap().slice(0,16384),output=module.decode(bytes,{numComponents:0});assert.equal(output.length,16384);assert.deepEqual(module.triageHeap().slice(0,16384),low);if(previous)assert.deepEqual(output,previous);previous=output;}
 console.log('PASS JPX alpha: six official PDFium matte colors; numeric 0/absent/1/2; current #687 low-memory and repeated-output controls');
}
module.exports={makePDF,expected};
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
