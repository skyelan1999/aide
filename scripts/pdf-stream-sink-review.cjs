'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),{MessageChannel}=require('node:worker_threads');
const turn=()=>new Promise(resolve=>setTimeout(resolve,10));
function once(port,predicate,send){return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{port.off('message',receive);reject(Error('owned message timeout'));},1000);function receive(message){if(predicate(message)){clearTimeout(timer);port.off('message',receive);resolve(message);}}port.on('message',receive);send();});}
async function runtime(file){const path=file==='worker'?process.env.AIDE_PDF_SINK_WORKER_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs':process.env.AIDE_PDF_SINK_API_BASELINE||'internal/server/web/vendor/pdfjs/pdf.min.mjs',source=fs.readFileSync(path,'utf8'),names=file==='worker'?'cg as Cancel,Cg as CancelComplete,lg as StreamError':'Dt as Cancel,Ft as CancelComplete,Lt as StreamError';return import('data:text/javascript;base64,'+Buffer.from(source+'\nexport{MessageHandler,'+names+'};').toString('base64'));}
async function main(){const rows=[];async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 for(const file of['worker','api']){
  const {MessageHandler,Cancel,CancelComplete,StreamError}=await runtime(file);
  await test('560 '+file+' repeated error/close/cancel streams release both maps',async()=>{
   const {port1,port2}=new MessageChannel(),server=new MessageHandler('server','client',port1),client=new MessageHandler('client','server',port2);port1.start();port2.start();
   try{server.on('fail',(_data,sink)=>{sink.error(Error('owned synthetic stream failure'));sink.error(Error('owned duplicate failure'));});server.on('close',(_data,sink)=>sink.close());server.on('wait',(_data,sink)=>{sink.onCancel=()=>{};});
    for(let i=0;i<100;i++){const reader=client.sendWithStream('fail',{}).getReader();await assert.rejects(reader.read(),/owned synthetic/);reader.releaseLock();}
    const closed=client.sendWithStream('close',{}).getReader();assert.equal((await closed.read()).done,true);closed.releaseLock();
    const cancelled=client.sendWithStream('wait',{}).getReader();await cancelled.cancel(Error('owned cancellation'));cancelled.releaseLock();await turn();assert.equal(Object.keys(server.streamSinks).length,0);assert.equal(Object.keys(client.streamControllers).length,0);return{errorStreams:100,retainedSinks:0,retainedControllers:0};
   }finally{server.destroy();client.destroy();port1.close();port2.close();}
  });
  await test('560 '+file+' cancel after terminal cleanup is acknowledged',async()=>{
   const {port1,port2}=new MessageChannel(),server=new MessageHandler('server','raw',port1);port1.start();port2.start();server.on('closed',(_data,sink)=>sink.close());
   try{await once(port2,m=>m.streamId===77&&m.success===true,()=>port2.postMessage({sourceName:'raw',targetName:'server',action:'closed',streamId:77,data:{},desiredSize:1}));assert.equal(Object.keys(server.streamSinks).length,0);const response=await once(port2,m=>m.stream===CancelComplete&&m.streamId===77,()=>port2.postMessage({sourceName:'raw',targetName:'server',stream:Cancel,streamId:77,reason:{name:'AbortException',message:'owned late cancellation'}}));assert.equal(response.success,true);}finally{server.destroy();port1.close();port2.close();}
  });
  await test('560 '+file+' failed error notification still deletes sink',async()=>{
   let receive;const port={addEventListener(_name,fn){receive=fn;},removeEventListener(){},postMessage(message){if(message.stream===StreamError)throw Error('owned closed-port notification');}},server=new MessageHandler('server','raw',port);server.on('fail',(_data,sink)=>sink.error(Error('owned failure')));
   try{receive({data:{sourceName:'raw',targetName:'server',action:'fail',streamId:12,data:{},desiredSize:1}});await turn();assert.equal(Object.keys(server.streamSinks).length,0);}finally{server.destroy();}
  });
 }
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
