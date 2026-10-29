'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),{header}=require('./pdf-decoder-followup-review.cjs');
function pageInfo(width,height){const body=Buffer.alloc(19);body.writeUInt32BE(width,0);body.writeUInt32BE(height,4);body[16]=4;return Buffer.concat([Buffer.from(header(0,1,false,19,48)),body]);}
function resizeSOF(bytes,width,height,sampling){const copy=Buffer.from(bytes),p=copy.indexOf(Buffer.from([255,192]));assert(p>=0);copy.writeUInt16BE(height,p+5);copy.writeUInt16BE(width,p+7);if(sampling!==undefined)for(let i=0;i<copy[p+9];i++)copy[p+11+i*3]=sampling;return copy;}
function moveJPX(bytes,origin=32){const copy=Buffer.from(bytes),p=copy.indexOf(Buffer.from([255,81]));assert(p>=0);for(const off of[6,10,14,18,30,34])copy.writeUInt32BE(copy.readUInt32BE(p+off)+origin,p+off);return copy;}
function minimalCFF(){return Buffer.from([1,0,4,4,0,1,1,1,5,84,101,115,116,0,1,1,1,3,24+139,17,0,0,0,0,0,2,1,1,2,3,14,14]);}
async function main(){
 const source=fs.readFileSync(process.env.AIDE_PDF_EMBEDDED_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {SimpleSegmentVisitor,Jbig2Image,Jbig2Stream,JpegImage,JpegStream,JpxImage,CFFParser,Stream,Dict}=await import('data:text/javascript;base64,'+Buffer.from(source+'\nexport{SimpleSegmentVisitor,Jbig2Image,Jbig2Stream,JpegImage,JpegStream,JpxImage,CFFParser,Stream,Dict};').toString('base64'));
 const jpeg=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpeg-dimensions.json','utf8')).images,rows=[];
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 const dict=values=>{const d=new Dict();for(const[k,v]of Object.entries(values))d.set(k,v);return d;};
 await test('535 embedded JBIG2 dimensions reject before page buffer allocation',()=>{
  const Native=globalThis.Uint8ClampedArray;let allocations=0;globalThis.Uint8ClampedArray=new Proxy(Native,{construct(){allocations++;throw Error('owned JBIG2 allocation trap');}});
  try{for(const info of [{width:8192,height:4096},{width:0,height:1},{width:1.5,height:1},{width:4294967295,height:1}])assert.throws(()=>new SimpleSegmentVisitor().onPageInformation(info),e=>e.name==='Jbig2Error');const visitor=new SimpleSegmentVisitor();visitor.expectedDimensions={width:1,height:1};assert.throws(()=>visitor.onPageInformation({width:32,height:1}),e=>e.name==='Jbig2Error'&&/differ/.test(e.message));assert.equal(allocations,0);}finally{globalThis.Uint8ClampedArray=Native;}
 });
 await test('535 actual chunk/stream dimensions and16Mipixel boundary',()=>{
  const image=new Jbig2Image(),data=pageInfo(1,1);assert.equal(image.parseChunks([{data,start:0,end:data.length}],{width:1,height:1}).length,1);assert.throws(()=>image.parseChunks([{data,start:0,end:data.length}],{width:2,height:1}),e=>e.name==='Jbig2Error');
  const stream=new Jbig2Stream(new Stream(data,0,data.length,dict({Width:2,Height:1})),data.length,null);assert.throws(()=>stream.decodeImage(),e=>e.name==='Jbig2Error');
  const visitor=new SimpleSegmentVisitor();visitor.onPageInformation({width:4096,height:4096,defaultPixelValue:1});assert.equal(visitor.buffer.length,2097152);assert.equal(visitor.buffer[0],255);
 });
 await test('536 SOF pixel/mismatch/sampling/block-plan limits precede component buffers',()=>{
  const Native=globalThis.Int16Array;let allocations=0;globalThis.Int16Array=new Proxy(Native,{construct(){allocations++;throw Error('owned JPEG block allocation trap');}});
  try{const rgb=Buffer.from(jpeg.find(x=>x.mode==='RGB').base64,'base64'),cmyk=Buffer.from(jpeg.find(x=>x.mode==='CMYK').base64,'base64');for(const [bytes,options]of [[resizeSOF(rgb,8192,4096),{}],[rgb,{expectedWidth:1,expectedHeight:1}],[resizeSOF(rgb,8,8,0),{}],[resizeSOF(cmyk,4096,4096,0x44),{}]])assert.throws(()=>new JpegImage(options).parse(bytes),e=>e.name==='JpegError');assert.equal(allocations,0);}finally{globalThis.Int16Array=Native;}
 });
 await test('536 gray/RGB/CMYK decoder controls and PDF stream expected dimensions',()=>{
  for(const f of jpeg){const bytes=Buffer.from(f.base64,'base64'),image=new JpegImage({expectedWidth:8,expectedHeight:8});image.parse(bytes);assert.equal(image.width,8);assert.equal(image.height,8);assert.equal(image.components.length,f.mode==='L'?1:f.mode==='RGB'?3:4);assert(image.components.every(x=>x.output.length>0));}
  const bytes=Buffer.from(jpeg.find(x=>x.mode==='RGB').base64,'base64'),make=width=>{const stream=new JpegStream(new Stream(bytes,0,bytes.length,dict({Width:width,Height:8})),bytes.length,null);stream.drawWidth=8;stream.drawHeight=8;stream.forceRGB=true;return stream;};assert.throws(()=>make(1).decodeImage(),e=>e.name==='JpegError');const pixels=make(8).decodeImage();assert.equal(pixels.length,192);assert(pixels[0]>245&&pixels[1]<10&&pixels[2]<10);
 });
 await test('537 nonzero JPX origins use extents in current upstream decoder',()=>{
  const fixtures=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-components.json','utf8')).images;let cases=0;for(const f of fixtures)for(const count of[0,1,3,4]){const bytes=Buffer.from(f.base64,'base64'),expected=JpxImage.decode(bytes,{numComponents:count}),shifted=JpxImage.decode(moveJPX(bytes),{numComponents:count});assert.deepEqual(shifted,expected);assert.equal(shifted.length,16*(count||4));cases++;}return{cases,origin:32,productChange:'Already covered by pinned decoder update for506; no further native patch'};
 });
 const parse=bytes=>new CFFParser(new Stream(Uint8Array.from(bytes)),{},false).parseIndex(0);
 await test('538 offSize/truncation/offset bounds reject before prolonged scans',()=>{
  for(const bytes of [[255,255,255],[0,1,0],[0,1,5],[0,2,1,1],[0,1,1,2,3,65],[0,2,1,1,3,2,65,66],[0,1,4,0,0,0,1,128,0,0,1]]){
   let reads=0;const target=Uint8Array.from(bytes),limited=new Proxy(target,{get(object,key){if(typeof key==='string'&&/^\d+$/.test(key)&&++reads>1024)throw Error('owned CFF read trap');const value=Reflect.get(object,key,object);return typeof value==='function'?value.bind(object):value;}});
   const parser=new CFFParser({getBytes:()=>limited},{},false);assert.throws(()=>parser.parseIndex(0),e=>e.name==='FormatError');assert(reads<1024);
  }
 });
 await test('538 valid empty, variable-offSize and zero-length INDEX objects',()=>{
  assert.equal(parse([0,0]).endPos,2);for(const size of[1,2,3,4]){const bytes=[0,1,size];for(const value of[1,2])for(let i=size-1;i>=0;i--)bytes.push((value/2**(i*8))&255);bytes.push(90);const result=parse(bytes);assert.equal(result.endPos,bytes.length);assert.deepEqual([...result.obj.get(0)],[90]);}
  const result=parse([0,2,1,1,1,2,90]);assert.equal(result.obj.count,2);assert.equal(result.obj.get(0).length,0);assert.deepEqual([...result.obj.get(1)],[90]);
  const font=new CFFParser(new Stream(minimalCFF()),{},false).parse();assert.deepEqual(font.names,['Test']);assert.equal(font.charStrings.count,2);
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module)main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
module.exports={pageInfo,resizeSOF,moveJPX,minimalCFF};
