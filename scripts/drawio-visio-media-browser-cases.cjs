'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
const {bmp}=require('./test_drawio_visio_media_errors.cjs');
module.exports=async function(frame,out,theme,file){
 if(file.endsWith('/importer.js'))await frame.addScriptTag({path:'internal/server/web/vendor/drawio/js/diagramly/vsdx/bmpDecoder.js'});
 await frame.evaluate(async validBmp=>{
  const page='<Pages><Page ID="0" Name="Owned BMP page" NameU="OwnedBMP"><PageSheet><Cell N="PageWidth" V="8"/><Cell N="PageHeight" V="8"/><Cell N="PageScale" V="1"/><Cell N="DrawingScale" V="1"/></PageSheet></Page></Pages>';
  async function zip(bad){const zip=new JSZip();zip.file('visio/document.xml','<VisioDocument>'+page+'</VisioDocument>');zip.file('visio/pages/pages.xml',page);zip.file('visio/media/one.bmp',bad?new Uint8Array(4):new Uint8Array(validBmp));if(bad)zip.file('visio/media/two.bmp',new Uint8Array(4));return zip.generateAsync({type:'uint8array',compression:'DEFLATE'});}
  const state={bad:await zip(true),good:await zip(false),pixels:[],capture:false,throwExport:false};
  const original=HTMLCanvasElement.prototype.toDataURL;
  HTMLCanvasElement.prototype.toDataURL=function(){
   if(state.capture&&this.width===1&&this.height===1){
    if(state.throwExport)throw Error('Owned canvas export failure');
    state.pixels.push(Array.from(this.getContext('2d').getImageData(0,0,1,1).data));
   }
   return original.apply(this,arguments);
  };
  state.restore=()=>{HTMLCanvasElement.prototype.toDataURL=original;};
  state.decode=async function(bad,filename,showError=false){
   const callbacks=[];state.capture=!bad;
   await new Promise((resolve,reject)=>{
    const timeout=setTimeout(()=>reject(Error('BMP import callback timeout')),5000);
    function done(value){callbacks.push(value);clearTimeout(timeout);setTimeout(resolve,40);}
    ownedUi.doImportVisio(new Blob([bad?state.bad:state.good]),xml=>done({kind:'success',xml}),error=>{if(showError)ownedUi.handleError(error);done({kind:'error',message:error?.message});},filename);
   });
   state.capture=false;return callbacks;
  };
  window.ownedMediaCase=state;
 },Array.from(bmp()));
 try{
  for(const filename of ['owned.vsdx','owned.vssx']){
   const failure=await frame.evaluate(filename=>ownedMediaCase.decode(true,filename,true),filename);
   assert.equal(failure.length,1);assert.equal(failure[0].kind,'error');assert.match(failure[0].message,/Invalid BMP/);
   const error=frame.getByText('Invalid BMP File',{exact:true});await error.waitFor();await frame.page().screenshot({path:path.join(out,theme+'-visio-bmp-error.png')});await frame.getByRole('button',{name:'OK',exact:true}).click();
   const recovered=await frame.evaluate(filename=>ownedMediaCase.decode(false,filename),filename);assert.equal(recovered.length,1);assert.equal(recovered[0].kind,'success');assert.match(recovered[0].xml,filename.endsWith('.vsdx')?/<diagram/:/^<mxlibrary>/);
  }
  const injected=await frame.evaluate(async()=>{ownedMediaCase.throwExport=true;try{return await ownedMediaCase.decode(false,'owned.vsdx');}finally{ownedMediaCase.throwExport=false;}});
  assert.equal(injected.length,1);assert.equal(injected[0].message,'Owned canvas export failure');
  const normal=await frame.evaluate(()=>ownedMediaCase.decode(false,'owned.vsdx'));assert.equal(normal[0].kind,'success');
  const pixels=await frame.evaluate(()=>ownedMediaCase.pixels);assert.ok(pixels.length>=3);for(const pixel of pixels)assert.deepEqual(pixel,[255,0,0,255]);
  return [{case:'actual VSDX/VSSX malformed BMP single error/UI, real 1px decoding/JPEG export, injected canvas error and same-runtime retry',file,theme,result:'PASS',onePixelConversions:pixels.length}];
 }finally{await frame.evaluate(()=>{ownedMediaCase.restore();delete window.ownedMediaCase;});}
};
