'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
const {method}=require('./test_drawio_trello_limits.cjs');
module.exports=async function(frame,out,theme){
 await frame.evaluate(()=>{if(typeof ownedTrelloClass!=='function')throw Error('pre-main Trello class not captured');window.ownedOriginalTrelloWrite=ownedTrelloClass.prototype.writeFile;});
 const checks=[];
 try{
  for(const file of ['diagramly/TrelloClient.js','app.min.js']){
   if(file.endsWith('TrelloClient.js'))await frame.addScriptTag({content:'(function(TrelloClient){'+method(file)+'})(window.ownedTrelloClass);'});
   else await frame.evaluate(()=>{ownedTrelloClass.prototype.writeFile=ownedOriginalTrelloWrite;});
   const result=await frame.evaluate(async()=>{
    const xhr=window.XMLHttpRequest,trello=window.Trello,hadTrello=Object.hasOwn(window,'Trello'),canvasFlag=Editor.useCanvasForExport;
    const uploads=[],calls={auth:0,deletes:0},limit=ownedTrelloClass.prototype.maxFileSize;
    if(limit!==10000000)throw Error('configured Trello limit changed');
    class Request{open(method,url){if(!url.startsWith('https://owned-upload.invalid/'))throw Error('unexpected mocked request');}send(form){uploads.push(form.get('file'));this.readyState=4;this.status=200;this.response={id:'owned-attachment'};queueMicrotask(()=>this.onreadystatechange());}}
    window.XMLHttpRequest=Request;window.Trello={key:()=> 'owned-test-key',token:()=> 'owned-test-token',del:(name,success)=>{calls.deletes++;success();}};
    const large=new Blob([new Uint8Array(limit+1)]),exact=new File([large.slice(0,limit)],'owned.png');
    const client=Object.create(ownedTrelloClass.prototype);client.baseUrl='https://owned-upload.invalid/';client.ui={formatFileSize:String,timeout:2000,getEmbeddedPng:success=>success('owned-export-placeholder'),base64ToBlob:()=>large,getCurrentFile:()=>null};client.authenticate=fn=>{calls.auth++;fn();};
    function write(data,showError=false){return new Promise(resolve=>client.writeFile('owned.xml',data,'owned-card',meta=>resolve({kind:'success',meta}),error=>{if(showError)ownedUi.handleError(error);resolve({kind:'error',message:error.message});}));}
    try{
     for(const data of [large,exact,'中'.repeat(Math.floor(limit/3)+1)]){
      const before=uploads.length,auth=calls.auth,result=await write(data);if(result.kind!=='error'||uploads.length!==before||calls.auth!==auth)throw Error('oversized upload bypassed byte guard');
     }
     for(const data of ['é中😀',new Blob(['owned'],{type:'image/png'}),'']){
      const size=typeof data==='string'?new Blob([data]).size:data.size,before=uploads.length,result=await write(data);
      if(result.kind!=='success'||uploads.length!==before+1||uploads.at(-1).size!==size)throw Error('valid payload bytes changed');
     }
     Editor.useCanvasForExport=true;
     for(const operation of ['insert','save']){
      const before=uploads.length,deletes=calls.deletes;
      const result=await new Promise(resolve=>{
       const success=()=>resolve('success'),error=()=>resolve('error');
       if(operation==='insert')client.insertFile('owned.png','<mxfile/>',success,error,false,'owned-card');
       else client.saveFile({meta:{compoundId:'owned-card'+client.SEPARATOR+'old',name:'owned.png'},getData:()=>'<mxfile/>'},success,error);
      });
      if(result!=='error'||uploads.length!==before||calls.deletes!==deletes)throw Error('PNG insert/save did not retain size guard');
     }
     const shown=await write(large,true);
     return {defaultClientDisabled:window.TrelloClient===null,message:shown.message,validUploads:uploads.length,configuredLimit:limit,pngInsertSaveRejected:true};
    }finally{window.XMLHttpRequest=xhr;if(hadTrello)window.Trello=trello;else delete window.Trello;Editor.useCanvasForExport=canvasFlag;}
   });
   assert.equal(result.defaultClientDisabled,true,'default cloud client stays disabled');assert.equal(result.validUploads,3);assert.equal(result.configuredLimit,10000000);
   const error=frame.getByText(result.message,{exact:true});await error.waitFor();await frame.page().screenshot({path:path.join(out,theme+'-trello-limit.png')});await frame.getByRole('button',{name:'OK',exact:true}).click();
   checks.push({case:'optional Trello source/captured production method with default integration still disabled, 10MB Blob/File/UTF8 rejection, real FormData bytes, PNG insert/save prevention and visible error; mock auth/XHR only',file,theme,result:'PASS',details:result});
  }
 }finally{await frame.evaluate(()=>{ownedTrelloClass.prototype.writeFile=ownedOriginalTrelloWrite;delete window.ownedOriginalTrelloWrite;delete window.ownedTrelloClass;});}
 return checks;
};
