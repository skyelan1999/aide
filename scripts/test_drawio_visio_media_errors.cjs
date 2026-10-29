'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {runtime,archive,files}=require('./test_drawio_visio_archive.cjs');
function decoder(file){
 const base='internal/server/web/vendor/drawio/js/';
 if(file===files[0])return fs.readFileSync(base+'diagramly/vsdx/bmpDecoder.js','utf8');
 const text=fs.readFileSync(base+file,'utf8'),a=text.indexOf('function BmpDecoder('),b=text.indexOf('var __extends=',a);assert.ok(a>=0&&b>a);return text.slice(a,b);
}
function bmp(){
 const data=Buffer.alloc(58);data.write('BM');data.writeUInt32LE(58,2);data.writeUInt32LE(54,10);data.writeUInt32LE(40,14);data.writeInt32LE(1,18);data.writeInt32LE(1,22);data.writeUInt16LE(1,26);data.writeUInt16LE(24,28);data.writeUInt32LE(4,34);data[56]=255;return data;
}
function prepare(file,canvasError=false){
 const images=[];
 const document={createElement:()=>({getContext:()=>({createImageData:(w,h)=>({data:new Uint8ClampedArray(w*h*4),width:w,height:h}),putImageData(image){if(canvasError)throw Error('owned canvas failed');images.push(image);}}),toDataURL:()=> 'data:image/jpeg;base64,b3duZWQ='})};
 const r=runtime(file,undefined,{document});vm.runInNewContext(decoder(file),r.context);
 r.Codec.prototype.RESPONSE_HEADER='<mxfile>';r.Codec.prototype.RESPONSE_END='</mxfile>';r.Codec.prototype.importNodes=()=>{};
 r.context.com.mxgraph.io.vsdx={mxVsdxModel:function(){this.getPages=()=>({entries:[]});}};
 return {r,images};
}
async function observe(r,data,stencil=false,withErrorHandler=true){
 const callbacks=[],unhandled=[];const onRejected=error=>unhandled.push(error.message);process.on('unhandledRejection',onRejected);
 try{
  const codec=stencil?new r.StencilCodec():new r.Codec();
  codec[stencil?'decodeVssx':'decodeVsdx'](data,xml=>callbacks.push({kind:'success',xml}),null,withErrorHandler?error=>callbacks.push({kind:'error',message:error?.message}):undefined);
  await new Promise(resolve=>setTimeout(resolve,80));return {callbacks,unhandled};
 }finally{process.removeListener('unhandledRejection',onRejected);}
}
async function main(){
 const invalid=await archive([['visio/document.xml','<VisioDocument/>'],['visio/media/one.bmp',Uint8Array.from([0,0,0,0])],['visio/media/two.bmp',Uint8Array.from([0,0,0,0])]]);
 const valid=await archive([['visio/document.xml','<VisioDocument/>'],['visio/media/one.bmp',bmp()]]);
 for(const file of files){
  for(const stencil of [false,true]){
   const {r}=prepare(file),result=await observe(r,invalid,stencil);console.log(JSON.stringify({file,stencil,...result}));
   assert.equal(result.callbacks.length,1,'malformed BMP must settle exactly once');assert.equal(result.callbacks[0].kind,'error');assert.match(result.callbacks[0].message,/Invalid BMP/);assert.deepEqual(result.unhandled,[]);
  }
  const good=prepare(file),recovery=await observe(good.r,valid);assert.deepEqual(recovery.unhandled,[]);assert.equal(recovery.callbacks.length,1);assert.equal(recovery.callbacks[0].kind,'success');assert.equal(good.images.length,1);assert.deepEqual(Array.from(good.images[0].data),[255,0,0,255]);
  const canvas=prepare(file,true),failed=await observe(canvas.r,valid);assert.equal(failed.callbacks.length,1);assert.equal(failed.callbacks[0].message,'owned canvas failed');assert.deepEqual(failed.unhandled,[]);
  for(const mode of ['reject','throw']){
   const injected=prepare(file),zip=await injected.r.Codec.loadArchive(valid);
   zip.file('visio/media/one.bmp').async=()=>{if(mode==='throw')throw Error('owned async failure');return Promise.reject(Error('owned async failure'));};
   injected.r.Codec.loadArchive=()=>Promise.resolve(zip);
   const failed=await observe(injected.r,valid);assert.equal(failed.callbacks.length,1);assert.equal(failed.callbacks[0].message,'owned async failure');assert.deepEqual(failed.unhandled,[]);
  }
  const fallback=await observe(prepare(file).r,invalid,false,false);assert.deepEqual(fallback.callbacks,[{kind:'success',xml:''}]);assert.deepEqual(fallback.unhandled,[]);
  console.log('PASS',file,'malformed BMP, duplicate failure suppression, real 1px BMP, canvas error and VSSX forwarding');
 }
}
module.exports={decoder,bmp,prepare,observe};if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
