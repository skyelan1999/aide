'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),preflight=require('./pdf-jpx-preflight.cjs');
const sample=Buffer.from(JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-components.json')).images[0].base64,'base64');
function box(type,payload){const b=Buffer.alloc(8+payload.length);b.writeUInt32BE(b.length);b.write(type,4);b.set(payload,8);return b;}
function marker(code,payload){const b=Buffer.alloc(4+payload.length);b.writeUInt16BE(code);b.writeUInt16BE(payload.length+2,2);b.set(payload,4);return b;}
let cs;for(let p=0;p<sample.length;){const n=sample.readUInt32BE(p);if(sample.toString('ascii',p+4,p+8)==='jp2c')cs=sample.subarray(p+8,p+n);p+=n;}
function rawSiz({width=4,height=4,tx=width,ty=height,components=1}={}){
 const b=Buffer.alloc(36+components*3);b.writeUInt32BE(width,2);b.writeUInt32BE(height,6);b.writeUInt32BE(tx,18);b.writeUInt32BE(ty,22);b.writeUInt16BE(components,34);
 for(let i=0;i<components;i++)b.set([7,1,1],36+i*3);
 return Buffer.concat([Buffer.from([255,79]),marker(0xff51,b)]);
}
function beforeTile(extra){const p=cs.indexOf(Buffer.from([255,144]));return Buffer.concat([cs.subarray(0,p),extra,cs.subarray(p)]);}
const ihdr=box('ihdr',Buffer.from([0,0,0,4,0,0,0,4,0,3,7,7,0,0]));
const wrap=children=>Buffer.concat([sample.subarray(0,32),box('jp2h',Buffer.concat([ihdr,...children])),box('jp2c',cs)]);
const coding=(layers,precinct)=>{const b=Buffer.from([precinct===undefined?0:1,0,layers>>8,layers&255,0,0,4,4,0,1,...(precinct===undefined?[]:[precinct])]);return marker(0xff52,b);};
async function main(){
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {oi,JpxImage}=await import('data:text/javascript;base64,'+Buffer.from(source+';export {oi,JpxImage};').toString('base64'));
 const module=oi({warn:()=>{}});let allocations=0;const malloc=module._malloc;module._malloc=n=>(allocations++,malloc(n));
 const esycc=rawSiz({components:3});esycc[46]=2;esycc[49]=2;
 const container=(stream,children)=>Buffer.concat([sample.subarray(0,32),box('jp2h',Buffer.concat([ihdr,...children])),box('jp2c',stream)]);
 // Header-only mutants test refusal before native parsing; they are not
 // represented as conforming serialized entropy/color fixtures.
 const tests=[
  ['subsampled e-sYCC',container(esycc,[box('colr',Buffer.from([1,0,0,0,0,0,24]))])],
  ['palette expanded planes',container(rawSiz({width:2048,height:2048}),[box('pclr',Buffer.from([0,1,8,...Array(8).fill(7),...Array(8).fill(0)]))])],
  ['FTYP',Buffer.concat([sample.subarray(0,12),box('ftyp',Buffer.alloc(65536)),sample.subarray(32)])],
  ['JP2H',wrap([box('free',Buffer.alloc(8*1024*1024))])],
  ['ICC',wrap([box('colr',Buffer.concat([Buffer.from([2,0,0]),Buffer.alloc(4*1024*1024+1)]))])],
  ['CDEF entries',wrap([box('cdef',Buffer.from([1,1]))])],
  ['PCLR precision',wrap([box('pclr',Buffer.from([0,1,1,32,0,0,0,0,0]))])],
  ['component count',rawSiz({components:16384})],
  ['tile count',rawSiz({width:65535,tx:1})],
  ['tile/component metadata',rawSiz({width:4096,tx:1,components:5})],
  ['component planes',rawSiz({width:305,height:30000,ty:396,components:3})],
  ['quality layers',Buffer.concat([rawSiz(),coding(65535)])],
  ['TERMALL quality layers',Buffer.concat([rawSiz(),coding(10000)])],
  ['packet iterator',Buffer.concat([rawSiz({width:4096,height:4096}),coding(1000,0)])],
  ['progression order',beforeTile(marker(0xff5f,Buffer.from([0,0,0,1,1,1,255])))],
  ['ROI style',beforeTile(marker(0xff5e,Buffer.from([0,255,0])))],
  ['TLM',beforeTile(marker(0xff55,Buffer.from([0,48,0,0,0,0,19])))],
  ['PLM',beforeTile(marker(0xff57,Buffer.from([0,1,128])))],
  ['marker count',beforeTile(Buffer.concat(Array.from({length:4097},()=>marker(0xff64,Buffer.alloc(0)))))],
  ['packed packet headers',beforeTile(Buffer.concat(Array.from({length:65},()=>marker(0xff60,Buffer.alloc(65000)))))],
  ['input bytes',new Uint8Array(64*1024*1024+1)],
  ['MCT tile copies',Buffer.concat([rawSiz({width:65535,tx:131}),...Array.from({length:20},()=>marker(0xff74,Buffer.alloc(12000)))])],
 ];
 for(const [label,bytes]of tests){
  allocations=0;assert.throws(()=>preflight(bytes),undefined,label);
  const result=module.decode(bytes,{numComponents:0});assert.equal(typeof result,'string',label);assert.match(result,/JPX preflight:/,label);assert.equal(allocations,0,label+' allocated native input');
 }
 // A later tile part may not replace the already-allocated coding geometry.
 const sot=(part,size)=>{const b=Buffer.alloc(8);b.writeUInt32BE(size,2);b[6]=part;b[7]=2;return marker(0xff90,b);};
 const firstPart=Buffer.concat([sot(0,14),Buffer.from([255,147])]);
 const coc=marker(0xff53,Buffer.from([0,1,0,4,4,0,1,0]));
 const later=Buffer.concat([rawSiz(),coding(1),firstPart,sot(1,14+coc.length),coc,Buffer.from([255,147,255,217])]);
 allocations=0;assert.match(module.decode(later,{numComponents:0}),/coding overrides after tile-part zero/);assert.equal(allocations,0);
 const inline=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-header-issues.json'));
 for(const row of inline){const bytes=Buffer.from(row.base64,'base64');if(row.number===629){allocations=0;assert.match(module.decode(bytes,{numComponents:0}),/JPX preflight:/);assert.equal(allocations,0);}else if(row.number===675){assert.throws(()=>preflight(bytes),/IHDR/);}else{
  const saved=Buffer.from(bytes),fixed=preflight(bytes);assert.notDeepEqual(fixed,bytes);assert.deepEqual(bytes,saved,'caller input mutated');
  const reference=Buffer.from(bytes);const second=reference.indexOf(Buffer.from('colr'),reference.indexOf(Buffer.from('colr'))+4);reference.write('free',second);
  assert.deepEqual(module.decode(bytes,{numComponents:0}),module.decode(reference,{numComponents:0}),'first COLR must determine interpretation');
 }}
 for(const options of [{numComponents:0},{numComponents:1},{numComponents:3},{numComponents:4}])assert.equal(typeof module.decode(sample,options),'object');
 assert.equal(JpxImage.decode(sample,{numComponents:3}).length,48);
 // Exactly supported palette precision remains accepted by the header guard.
 preflight(wrap([box('pclr',Buffer.from([0,1,1,30,0,0,0,0]))]));
 console.log('PASS JPX header preflight: '+tests.length+' allocation-free rejection cases; exact #675/#823 fixtures; 4 native controls; precision boundary');
}
main().catch(e=>{console.error(e);process.exitCode=1;});
