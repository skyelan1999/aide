'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
function packetInput(base,type,size){
 const payload=type==='ppm'?Buffer.concat([Buffer.from([size>>>24,size>>>16&255,size>>>8&255,size&255]),Buffer.alloc(size)]):Buffer.alloc(size);
 const markers=[];for(let p=0,n=0;p<payload.length;p+=65532,n++){const part=payload.subarray(p,p+65532),marker=Buffer.alloc(5+part.length);marker.writeUInt16BE(type==='ppm'?0xff60:0xff61);marker.writeUInt16BE(3+part.length,2);marker[4]=n;marker.set(part,5);markers.push(marker);}
 const body=Buffer.concat(markers),sot=base.indexOf(Buffer.from([255,144]));
 const result=type==='ppm'?Buffer.concat([base.subarray(0,sot),body,base.subarray(sot)]):Buffer.concat([base.subarray(0,sot+12),body,base.subarray(sot+12)]);
 if(type==='ppt')result.writeUInt32BE(base.readUInt32BE(sot+6)+body.length,sot+6);
 const control=Buffer.from(result);for(let p=type==='ppm'?sot:sot+12,end=p+body.length;p<end;){control.writeUInt16BE(0xff64,p);p+=2+control.readUInt16BE(p+2);}
 return{result,control,markers:markers.length};
}
(async()=>{
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {oi}=await import('data:text/javascript;base64,'+Buffer.from(source+';export {oi};').toString('base64'));
 const row=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-native-fixes.json')).images.find(x=>x.name==='packet-header-empty-control.j2k'),base=Buffer.from(row.base64,'base64');
 const size=2*1024*1024,results=[];
 for(const type of ['ppm','ppt']){
  const {result,control,markers}=packetInput(base,type,size),m=oi({warn:()=>{}});
  const expected=m.decode(control,{numComponents:0});assert(expected instanceof Uint8ClampedArray);const before=m.aideAllocationBytes;
  const actual=m.decode(result,{numComponents:0});assert(actual instanceof Uint8ClampedArray,actual);assert.deepEqual(actual,expected);assert.deepEqual([...actual],[128,128,128,255]);
  const added=m.aideAllocationBytes-before;assert(added>=size&&added<size+256*1024,type+' retained an extra complete per-marker payload copy: '+added);
  results.push({type,payload:size,markers,additionalNativeRequestedBytes:added});
  if(process.env.AIDE_PACKET_FIXTURE_DIR){fs.mkdirSync(process.env.AIDE_PACKET_FIXTURE_DIR,{recursive:true});fs.writeFileSync(process.env.AIDE_PACKET_FIXTURE_DIR+'/'+type+'.j2k',result);const duplicate=Buffer.from(result),first=type==='ppm'?base.indexOf(Buffer.from([255,144])):base.indexOf(Buffer.from([255,144]))+12;const next=first+2+duplicate.readUInt16BE(first+2);duplicate[next+4]=0;fs.writeFileSync(process.env.AIDE_PACKET_FIXTURE_DIR+'/'+type+'-duplicate.j2k',duplicate);}
 }
 console.log(JSON.stringify({status:'PASS',sameSizeCOMControls:true,normalPixelsPreserved:true,results}));
})().catch(e=>{console.error(e);process.exitCode=1;});
