'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const api=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.min.mjs','utf8')+'\nexport{MessageHandler};',worker=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8')+'\nexport{MessageHandler,cg as Cancel,Cg as CancelComplete};';
 const probe=`import {MessageHandler,Cancel,CancelComplete} from '/worker-source.mjs';let server;self.addEventListener('message',event=>{const data=event.data;if(data.owned==='start'){const port=event.ports[0];server=new MessageHandler('server','client',port);port.start();server.on('fail',(_data,sink)=>sink.error(Error('owned stream failure')));server.on('bufferFail',(_data,sink)=>{sink.enqueue(new Uint8Array([1,2,3,4]),4);sink.error(Error('owned buffered failure'));});server.on('close',(_data,sink)=>sink.close());server.on('wait',(_data,sink)=>{sink.onCancel=()=>{};});self.postMessage({owned:'ready',Cancel,CancelComplete});}if(data.owned==='stats')self.postMessage({owned:'stats',id:data.id,sinks:Object.keys(server.streamSinks).length});});`;
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});
 try{
  const context=await browser.newContext(),page=await context.newPage(),errors=[];page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>{const u=new URL(route.request().url());if(u.origin!=='https://aide-streams.test')return route.abort();const body=u.pathname==='/'?'<!doctype html><title>Owned stream lifecycle</title>':u.pathname==='/api-source.mjs'?api:u.pathname==='/worker-source.mjs'?worker:u.pathname==='/probe.mjs'?probe:null;return body===null?route.fulfill({status:404,body:''}):route.fulfill({contentType:u.pathname==='/'?'text/html':'application/javascript',body});});
  await page.goto('https://aide-streams.test/');
  const rows=await page.evaluate(async()=>{
   const {MessageHandler}=await import('/api-source.mjs'),worker=new Worker('/probe.mjs',{type:'module'}),channel=new MessageChannel(),client=new MessageHandler('client','server',channel.port1),rows=[];channel.port1.start();
   function wait(target,predicate,send){return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{target.removeEventListener('message',receive);reject(Error('owned stream probe timeout'));},5000);function receive(event){if(predicate(event.data)){clearTimeout(timer);target.removeEventListener('message',receive);resolve(event.data);}}target.addEventListener('message',receive);send();});}
   let id=0;const stats=()=>{const value=++id;return wait(worker,m=>m.owned==='stats'&&m.id===value,()=>worker.postMessage({owned:'stats',id:value}));};
   const drained=async()=>{for(let i=0;i<100&&Object.keys(client.streamControllers).length;i++)await new Promise(r=>setTimeout(r,1));if(Object.keys(client.streamControllers).length)throw Error('owned controller retention');const result=await stats();if(result.sinks)throw Error('owned sink retention '+result.sinks);return result;};
   try{
    const ready=await wait(worker,m=>m.owned==='ready',()=>worker.postMessage({owned:'start'},[channel.port2]));
    for(let i=0;i<100;i++){const reader=client.sendWithStream('fail',{}).getReader();try{await reader.read();throw Error('owned failure did not propagate');}catch(e){if(!e.message.includes('owned stream failure'))throw e;}finally{reader.releaseLock();}}
    rows.push({case:'100actualworkererrorstreams',status:'PASS',...(await drained())});
    const buffered=client.sendWithStream('bufferFail',{}).getReader();let bytes=0,failed=false;try{for(;;){const result=await buffered.read();if(result.done)break;bytes+=result.value.length;}}catch(e){if(!e.message.includes('owned buffered failure'))throw e;failed=true;}finally{buffered.releaseLock();}if(!failed||bytes>4)throw Error('owned backpressure failure mismatch');rows.push({case:'backpressureerror',status:'PASS',bytes,...(await drained())});
    const closed=client.sendWithStream('close',{}).getReader();if(!(await closed.read()).done)throw Error('owned close failed');closed.releaseLock();const cancelled=client.sendWithStream('wait',{}).getReader();await cancelled.cancel(Error('owned cancellation'));cancelled.releaseLock();rows.push({case:'closeandcancel',status:'PASS',...(await drained())});
    client.destroy();const port=channel.port1;
    await wait(port,m=>m.streamId===77&&m.success===true,()=>port.postMessage({sourceName:'client',targetName:'server',action:'close',streamId:77,data:{},desiredSize:1}));
    const ack=await wait(port,m=>m.stream===ready.CancelComplete&&m.streamId===77,()=>port.postMessage({sourceName:'client',targetName:'server',stream:ready.Cancel,streamId:77,reason:{name:'AbortException',message:'owned late cancellation'}}));if(!ack.success)throw Error('owned late cancel not acknowledged');rows.push({case:'terminalcancelrace',status:'PASS',...(await stats())});return rows;
   }finally{client.destroy();channel.port1.close();worker.terminate();}
  });
  assert.deepEqual(errors,[]);assert(rows.every(r=>r.status==='PASS'&&r.sinks===0));fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({rows,errors,scope:'Actual API MessageHandler in Chromium connected to actual worker MessageHandler over a transferred MessagePort; synthetic streams only.'},null,2)+'\n');console.log(JSON.stringify(rows));await context.close();
 }finally{await browser.close();}
}
main().catch(e=>{console.error(e);process.exitCode=1;});
