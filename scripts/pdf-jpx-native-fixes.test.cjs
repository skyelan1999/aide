'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),crypto=require('node:crypto');

function offsetVariant(input,kind,value){
 const bytes=Buffer.from(input);let p=2;
 while(p+4<bytes.length){
  const marker=bytes.readUInt16BE(p),length=bytes.readUInt16BE(p+2);
  if(marker===0xff90)break;
  if(marker===0xff74&&(bytes.readUInt16BE(p+6)&255)===2){
   const data=Buffer.alloc(kind===0?6:12);if(kind===0)data.writeInt16BE(value);else if(kind===1)data.writeInt32BE(value);else data.writeFloatBE(value);
   const head=Buffer.from(bytes.subarray(p,p+10));head.writeUInt16BE(8+data.length,2);head.writeUInt16BE((kind<<10)|(2<<8)|2,6);
   return Buffer.concat([bytes.subarray(0,p),head,data,bytes.subarray(p+2+length)]);
  }
  p+=2+length;
 }
 throw Error('retained offset marker missing');
}

(async()=>{
 const fixture=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-native-fixes.json'));
 const bytes=name=>{const row=fixture.images.find(r=>r.name===name);assert(row,name);const b=Buffer.from(row.base64,'base64');assert.equal(crypto.createHash('sha256').update(b).digest('hex'),row.sha256);return b;};
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {oi,JpxImage}=await import('data:text/javascript;base64,'+Buffer.from(source+';export {oi,JpxImage};').toString('base64'));
 const module=oi({warn:()=>{}}),decode=(input,numComponents=0)=>{const r=module.decode(input,{numComponents});assert(r instanceof Uint8ClampedArray,typeof r==='string'?r:'invalid decoder output');return r;};
 const signed=bytes('627-reported.jp2'),unsigned=bytes('627-unsigned-control.jp2');
 for(const count of [0,1])assert.deepEqual(decode(signed,count),decode(unsigned,count),'signed gray midpoint lost at '+count);
 const dc=decode(bytes('653-reproducer.j2k'),1);assert.deepEqual([...dc.slice(0,4)],[191,255,96,112]);assert.deepEqual([...dc.slice(8,12)],[191,255,96,112]);
 const offsets=[100,100.75,101].map(n=>decode(bytes('625-offset-'+String(n).replace('.','p')+'.j2k'),3));
 assert.deepEqual(offsets.map(r=>r[0]),[11,12,12]);assert.notDeepEqual(offsets[0],offsets[1]);assert.deepEqual(offsets[1],offsets[2]);
 const original=bytes('623-negative-int16-offset-source.j2k');
 assert.deepEqual(decode(original,3),decode(offsetVariant(original,1,-1),3),'int16 -1 must equal int32 -1');
 assert.deepEqual([...decode(original,3).slice(0,3)],[0,19,0]);
 // A positive, non-clipped offset keeps the record-selection check independent
 // of the separately fixed signed-offset clipping case.
 const selection=['623-negative-int16-offset-source.j2k','623-single-selected-mcc-index4-control.j2k','623-second-selected-mcc-index4.j2k'].map(n=>decode(offsetVariant(bytes(n),0,100),3));
 assert.equal(selection[0][0],11);assert.deepEqual(selection[1],selection[0]);assert.deepEqual(selection[2],selection[0]);
 for(const value of [NaN,Infinity,-Infinity])assert.equal(typeof module.decode(offsetVariant(original,2,value),{numComponents:3}),'string','nonfinite MCT offset accepted');
 assert.deepEqual(decode(bytes('708-singleht-one-codeblock-cleanup-sigprop-separated-by-layer.j2k'),1),decode(bytes('708-singleht-one-layer-control.j2k'),1));
 const bad=bytes('642-reported.j2k');assert.match(module.decode(bad,{numComponents:0}),/segmentation symbol|decode the image/);
 assert.throws(()=>JpxImage.decode(bad,{numComponents:0}),/segmentation symbol|decode the image/);
 assert.equal(decode(bytes('642-no-segsym-control.j2k')).length,256);
 const es=fixture.images.find(r=>r.issue===693);assert.deepEqual(decode(bytes(es.name)),new Uint8ClampedArray(Buffer.from(es.expectedRGBA,'base64')),'PIMA-derived full raster mismatch');
 console.log(JSON.stringify({status:'PASS',issues:[582,623,625,627,642,653,693,708],signedGray:'auto and explicit',fractionalOffsets:offsets.map(r=>r[0]),laterMCC:'matches non-clipped selected control',layerSplit:'all samples identical',eSYCC:'all 512 RGBA bytes match independent PIMA reference',invalidSegsym:'decoder error, then valid recovery'}));
})().catch(e=>{console.error(e);process.exitCode=1;});
