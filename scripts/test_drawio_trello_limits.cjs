'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const files=['diagramly/TrelloClient.js','app.min.js'];
function method(file){
 const text=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');
 const marker='/* AIDE Trello byte limit #180 */';let a=text.indexOf(marker);
 if(a<0)a=text.indexOf(file===files[0]?'TrelloClient.prototype.writeFile =':'TrelloClient.prototype.writeFile=function');
 const b=text.indexOf(file===files[0]?'TrelloClient.prototype.pickLibrary =':'TrelloClient.prototype.pickLibrary=',a);assert.ok(a>=0&&b>a);return text.slice(a,b);
}
function run(file,data,limit=16){
 function Client(){}
 const calls={auth:0,uploads:[],errors:[],success:0};
 class Request{open(){}send(form){calls.uploads.push(form.get('file'));this.readyState=4;this.status=200;this.response={id:'owned-attachment'};this.onreadystatechange();}}
 const context={TrelloClient:Client,Blob,File,FormData,XMLHttpRequest:Request,Trello:{key:()=> 'owned-test-key',token:()=> 'owned-test-token'},window:{setTimeout,clearTimeout},mxUtils:{bind:(self,fn)=>fn.bind(self)},mxResources:{get:key=>key},App:{ERROR_TIMEOUT:'owned-timeout'}};
 vm.runInNewContext(method(file),context);
 const client=new Client();Object.assign(client,{maxFileSize:limit,baseUrl:'https://owned-upload.invalid/',SEPARATOR:'|',ui:{formatFileSize:String,timeout:1000},authenticate(fn){calls.auth++;fn();}});
 client.writeFile('owned.xml',data,'owned-card',()=>calls.success++,error=>calls.errors.push(error));return calls;
}
function main(){
 const cases=[['oversized Blob',new Blob([new Uint8Array(17)])],['exact File',new File([new Uint8Array(16)],'owned.png')],['UTF8 Chinese','中'.repeat(6)],['UTF8 emoji','😀'.repeat(5)],['exact UTF8','😀'.repeat(4)],['ASCII below','x'.repeat(15)],['ASCII exact','x'.repeat(16)],['Blob below',new Blob([new Uint8Array(15)])],['empty',''],['mixed UTF8','é中😀']];
 const mismatches=[];
 for(const file of files){
  for(const [name,data]of cases){
   const size=typeof data==='string'?new Blob([data]).size:data.size,expected=size>=16,calls=run(file,data);
   if(expected){if(calls.errors.length!==1||calls.uploads.length||calls.auth||calls.success)mismatches.push({file,name,size,expected:'reject before auth/upload',actual:{errors:calls.errors.length,uploads:calls.uploads.length,auth:calls.auth,success:calls.success}});else assert.ok(calls.errors[0].message.includes(String(size)),'error displays actual bytes');}
   else{assert.equal(calls.errors.length,0);assert.equal(calls.success,1);assert.equal(calls.uploads.length,1);assert.equal(calls.uploads[0].size,size,'guard and submitted Blob must match');}
  }
 }
 if(mismatches.length)console.log('Trello byte diagnostics '+JSON.stringify(mismatches));assert.deepEqual(mismatches,[]);
 console.log('PASS source/bundle Trello Blob, File, UTF8/ASCII, exact boundaries and submitted payload byte identity');
}
module.exports={method,run,files};if(require.main===module)main();
