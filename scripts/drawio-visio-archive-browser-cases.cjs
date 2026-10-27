'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {files}=require('./test_drawio_visio_archive.cjs');
function importer(file){
 const text=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');
 if(file===files[0])return text;
 const a=text.indexOf('var __extends='),b=text.indexOf(';(function(){function g(){return{m11:',a);
 assert.ok(a>=0&&b>a,'locate full distributed Visio converter');return text.slice(a,b+1);
}
module.exports=async function(frame,out,theme){
 const checks=[];
 if(!await frame.evaluate(()=>typeof JSZip!=='undefined'))await frame.addScriptTag({path:'internal/server/web/vendor/drawio/js/jszip/jszip.min.js'});
 for(const file of files){
  await frame.evaluate(()=>{window.com=undefined;});
  await frame.addScriptTag({content:importer(file)});
  const result=await frame.evaluate(async()=>{
   const page='<Pages><Page ID="0" Name="Owned Visio page" NameU="OwnedPage"><PageSheet><Cell N="PageWidth" V="8"/><Cell N="PageHeight" V="8"/><Cell N="PageScale" V="1"/><Cell N="DrawingScale" V="1"/></PageSheet></Page></Pages>';
   const small=new JSZip();small.file('visio/document.xml','<VisioDocument>'+page+'</VisioDocument>');small.file('visio/pages/pages.xml',page);
   const good=await small.generateAsync({type:'uint8array',compression:'DEFLATE'});
   const bomb=new JSZip();bomb.file('visio/document.xml','<VisioDocument><!--'+'x'.repeat(8*1024*1024)+'--></VisioDocument>');
   const bad=await bomb.generateAsync({type:'uint8array',compression:'DEFLATE'});
   const results=[];
   async function decode(bytes,filename,showError=false){
    return new Promise((resolve,reject)=>{
     let calls=0;
     const timeout=setTimeout(()=>reject(Error('Visio callback timeout '+filename+' bytes='+bytes.length)),10000);
     ownedUi.doImportVisio(new Blob([bytes]),xml=>{calls++;clearTimeout(timeout);setTimeout(()=>calls===1?resolve({xml}):reject(Error('multiple Visio callbacks')),0);},error=>{calls++;clearTimeout(timeout);if(showError)ownedUi.handleError(error);setTimeout(()=>calls===1?resolve({error:{message:error?.message,code:error?.code}}):reject(Error('multiple Visio callbacks')),0);},filename);
    });
   }
   for(const filename of ['owned.vsdx','owned.vssx']){
    const failure=await decode(bad,filename,filename.endsWith('.vssx'));
    if(failure.error?.code!=='AIDE_VISIO_LIMIT')throw Error('missing ZIP budget error '+JSON.stringify(failure));
    const recovery=await decode(good,filename);
    if(recovery.error)throw Error('valid import failed '+JSON.stringify(recovery));
    if(filename.endsWith('.vsdx')){
     if(!recovery.xml.includes('<diagram')||!recovery.xml.includes('OwnedPage'))throw Error('missing converted Visio page');
     ownedUi.setFileData(recovery.xml);
    }else if(recovery.xml!=='<mxlibrary>[]</mxlibrary>')throw Error('unexpected empty stencil library '+recovery.xml);
    results.push({filename,code:failure.error.code,converted:recovery.xml.length});
   }
   return results;
  });
  const dialog=frame.getByText('Visio import resource limit exceeded (8 MiB input/member, 16 MiB expanded, 1024 entries)',{exact:true});
  await dialog.waitFor();await frame.page().screenshot({path:path.join(out,theme+'-visio-limit.png')});
  await frame.getByRole('button',{name:'OK',exact:true}).click();
  assert.equal(await dialog.count(),0);
  checks.push({case:'actual doImportVisio VSDX/VSSX oversized error and same-runtime valid page/library recovery; visible import error dialog',file,theme,result:'PASS',details:result});
 }
 return checks;
};
module.exports.importer=importer;
