'use strict';
// Actual Chromium audio capture/worklet/playback with a page-owned protocol fixture.
// Chromium's synthetic microphone is mandatory; no real device or provider is used.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
(async()=>{
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const root=path.resolve('internal/server/web');let browser;
 const server=http.createServer((req,res)=>{const name=new URL(req.url,'http://localhost').pathname;if(name==='/'){res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><title>Owned call regression</title><main></main><script src="/assistant-realtime.js"></script><script src="/assistant-call.js"></script>');return;}
  if(!['/assistant-realtime.js','/assistant-call.js','/assistant-pcm-worklet.js'].includes(name)){res.statusCode=404;res.end();return;}res.setHeader('Content-Type','application/javascript');res.end(fs.readFileSync(root+name));});
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;const errors=[],commands=[];let peer,packets=0;
 try{
  browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream']});
  const context=await browser.newContext({permissions:['microphone'],viewport:{width:1000,height:700}});const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.routeWebSocket('**/assistant-call/socket',socket=>{peer=socket;socket.onMessage(raw=>{const e=JSON.parse(raw);if(e.type==='input_audio_buffer.append'){assert.equal(Buffer.from(e.audio,'base64').length,640);packets++;}else commands.push(e.type);});setTimeout(()=>socket.send(JSON.stringify({type:'aide.ready'})),30);});
  await page.goto(base,{waitUntil:'networkidle'});
  await page.evaluate(()=>{
   window.fixture={valid:true,phases:[],errors:[]};let call;
   const live=AideAssistantRealtime.create({valid:()=>fixture.valid,session:()=> 'owned',token:()=> 'synthetic-call-token',state:s=>{fixture.phases.push(s.phase);call?.paint(s);},error:s=>fixture.errors.push(s)});
   call=AideAssistantCall.create({valid:()=>fixture.valid,supported:()=>true,live:()=>live,unlockAudio(){},cancelSpeech(){},error:s=>fixture.errors.push(s)});
   fixture.call=call;fixture.live=live;document.querySelector('main').append(call.element);
  });
  await page.getByRole('button',{name:'语音通话'}).click();await page.waitForFunction(()=>fixture.live.snapshot().phase==='listening');await page.waitForTimeout(150);assert(packets>0,'actual AudioWorklet must produce PCM');
  peer.send(JSON.stringify({type:'response.created',response:{id:'owned-response'}}));peer.send(JSON.stringify({type:'response.audio.delta',response_id:'owned-response',item_id:'owned-item',delta:Buffer.alloc(96000).toString('base64')}));await page.waitForFunction(()=>fixture.live.snapshot().phase==='speaking');
  peer.send(JSON.stringify({type:'input_audio_buffer.speech_started'}));await page.waitForFunction(()=>fixture.live.snapshot().phase==='listening');assert(commands.includes('response.cancel'));assert(commands.includes('conversation.item.truncate'));
  await page.getByRole('button',{name:'静音',exact:true}).click();assert.equal(await page.evaluate(()=>fixture.live.snapshot().microphone),false);await page.getByRole('button',{name:'取消静音',exact:true}).click();await page.waitForFunction(()=>fixture.live.snapshot().microphone);
  await page.screenshot({path:path.join(out,'call.png')});await page.getByRole('button',{name:'挂断',exact:true}).click();assert.equal(await page.evaluate(()=>fixture.live.snapshot().enabled),false);
  await page.getByRole('button',{name:'语音通话'}).click();await page.waitForFunction(()=>fixture.live.snapshot().phase==='listening');await page.evaluate(()=>{fixture.valid=false;fixture.call.paint(fixture.live.snapshot());});assert.equal(await page.evaluate(()=>fixture.live.snapshot().enabled),false);assert.equal(await page.evaluate(()=>fixture.call.active()),false);
  const details=await page.evaluate(()=>({phases:fixture.phases,errors:fixture.errors}));assert.deepEqual(details.errors,[]);assert.deepEqual(errors,[]);
  const proof={status:'PASS',scope:'Actual Chromium synthetic microphone, AudioWorklet PCM capture and WebAudio playback; Playwright intercepts only the owned WebSocket protocol. No paid provider, real microphone or full Aide host.',packets,commands,details,browser:browser.version()};fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(proof,null,2)+'\n');console.log(JSON.stringify(proof));
 }finally{if(browser)await browser.close();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1;});
