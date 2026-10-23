const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const context={window:{},AbortController,setTimeout,clearTimeout,Date};vm.createContext(context);vm.runInContext(fs.readFileSync('internal/server/web/assistant-live.js','utf8'),context);
const tick=async()=>{for(let i=0;i<25;i++)await Promise.resolve();};
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};};
function fixture(){
 let serial=0,valid=true,stops=0,interruptions=0;const timers=new Map(),recs=[],sent=[],spoken=[],events=[];let records=[],send=async()=>({reply:'好'}),speak=async()=>{},opening=null;
 const h={valid:()=>valid,setTimeout:(fn,ms)=>{timers.set(++serial,{fn,ms});return serial;},clearTimeout:id=>timers.delete(id),state:()=>{},error:()=>{},cancelSpeech:()=>{interruptions++;},
 openMicrophone:async()=>{if(opening)return opening.promise;const rec={start(){this.started=true;},abort(){this.aborted=true;}};recs.push(rec);return {recognition:rec,stream:{getTracks:()=>[{stop(){stops++;}}],getAudioTracks:()=>[{}]}};},
 send:async(text,signal)=>{sent.push({text,signal});return send(text,signal);},speak:async text=>{spoken.push(text);return speak(text);},reply:()=>{},watch:async()=>{if(records instanceof Error)throw records;return records;},events:items=>events.push(...items)};
 const live=context.window.AideAssistantLive.create(h);
 return {live,recs,sent,spoken,events,timers,setRecords:v=>records=v,setSend:v=>send=v,setSpeak:v=>speak=v,setOpening:v=>opening=v,setValid:v=>valid=v,stops:()=>stops,interruptions:()=>interruptions,
 async timer(ms){const item=[...timers].find(([,v])=>v.ms===ms);assert.ok(item,`missing timer ${ms}`);timers.delete(item[0]);item[1].fn();await tick();},
 emit(text,isFinal=true){const rec=recs.at(-1);rec.onresult({resultIndex:0,results:[{isFinal,0:{transcript:text}}]});}};
}
(async()=>{
 const f=fixture(),reply=deferred(),speech=deferred();f.setSend(()=>reply.promise);f.setSpeak(()=>speech.promise);
 await f.live.start(true);f.recs[0].onstart();assert.equal(f.live.snapshot().phase,'listening');f.emit('帮我查看消息');await f.timer(1100);
 assert.equal(f.sent.length,1);assert.equal(f.sent[0].text,'帮我查看消息');assert.equal(f.recs[0].aborted,true);assert.equal(f.stops(),1);
 reply.resolve({reply:'暂时没有新消息'});await tick();assert.equal(f.live.snapshot().phase,'speaking');assert.equal(f.recs.length,2,'recognizer stays available during playback');
 f.emit('暂时没有新消息');await tick();assert.equal(f.interruptions(),0,'own playback echo is ignored');
 f.emit('等一下',false);await tick();assert.equal(f.interruptions(),1,'new interim voice interrupts immediately before final ASR');
 speech.resolve();await tick();assert.equal(f.recs.length,2,'recognizer is reused after interrupted reply');
 f.emit('停止查询');await f.timer(1100);assert.equal(f.sent.at(-1).text,'停止查询','interrupted speech is submitted once');
 f.live.stop();await f.live.start(true);
 const current=f.recs.length;
 f.recs.at(-1).onend();await f.timer(400);assert.equal(f.recs.length,current+1,'browser recognition end reconnects');
 f.live.stop();assert.equal(f.live.snapshot().enabled,false);assert.equal(f.timers.size,0);
 const late=fixture(),permission=deferred();late.setOpening(permission);const starting=late.live.start(true);late.live.stop();let freed=0;const rec={start(){throw Error('stale microphone started');}};
 permission.resolve({recognition:rec,stream:{getTracks:()=>[{stop(){freed++;}}]}});await starting;assert.equal(freed,1,'stop releases late permission result');
 const pending=fixture(),response=deferred();pending.setSend(()=>response.promise);await pending.live.start(true);pending.emit('好');await pending.timer(1100);pending.live.stop();response.resolve({reply:'late'});await tick();assert.equal(pending.spoken.length,0);assert.equal(pending.recs.length,1);assert.equal(pending.sent[0].signal.aborted,true);
 const watcher=fixture();watcher.setRecords([{id:'old',text:'旧事件'}]);await watcher.live.start(false);await tick();assert.equal(watcher.events.length,0,'initial state is baseline');
 watcher.setRecords([{id:'old',text:'旧事件'},{id:'new',text:'工作已完成'}]);await watcher.timer(10000);await tick();assert.equal(watcher.events.length,1);assert.equal(watcher.spoken.length,1);
 await watcher.timer(10000);assert.equal(watcher.events.length,1,'same event does not repeat');watcher.live.pause();assert.equal(watcher.timers.size,0);
 watcher.live.resume();await tick();assert.equal(watcher.live.snapshot().paused,false);watcher.setValid(false);await watcher.timer(10000);assert.equal(watcher.live.snapshot().enabled,false,'session/lock invalidation stops polling');
 const manual=fixture();await manual.live.start(false);manual.live.reply('Typed reply');await tick();assert.equal(manual.spoken.at(-1),'Typed reply','typed replies in companion mode are spoken');manual.live.pause();const spokenCount=manual.spoken.length;manual.live.reply('Paused reply');await tick();assert.equal(manual.spoken.length,spokenCount);manual.live.stop();
 const denied=fixture();denied.setOpening({promise:Promise.reject(new Error('permission denied'))});await denied.live.start(true);assert.equal(denied.live.snapshot().enabled,true,'microphone failure must not disable work coordination');assert.equal(denied.live.snapshot().microphone,false);assert(denied.timers.size>0,'work heartbeat remains active');denied.live.stop();
 const independent=fixture();await independent.live.start(false);independent.live.setMicrophone(true);await tick();assert.equal(independent.recs.length,1);independent.live.setMicrophone(false);assert.equal(independent.live.snapshot().enabled,true);assert.equal(independent.live.snapshot().phase,'watching');independent.setRecords([{id:'blocker',kind:'task',status:'awaiting_approval',priority:3,text:'Approval needed'}]);await independent.timer(10000);await tick();assert.equal(independent.events.length,1);assert.equal(independent.spoken.at(-1),'Approval needed');await independent.timer(10000);assert.equal(independent.events.length,1,'unchanged heartbeat stays quiet');independent.live.stop();
 const firstBlocker=fixture();firstBlocker.setRecords([{id:'urgent',kind:'task',status:'failed',priority:3,text:'Failed task'},{id:'running',kind:'task',status:'running',priority:1,text:'Working'}]);await firstBlocker.live.start(false);await tick();assert.equal(firstBlocker.events.length,1,'initial blockers must not be swallowed by baseline');firstBlocker.live.stop();
 const reconnect=fixture();reconnect.setRecords(Error('offline'));await reconnect.live.start(false);await tick();await reconnect.timer(10000);await reconnect.timer(10000);assert.equal(reconnect.live.snapshot().phase,'recovering');assert.equal(reconnect.live.snapshot().enabled,true);reconnect.setRecords([]);await reconnect.timer(30000);assert.equal(reconnect.live.snapshot().phase,'watching');reconnect.setRecords(Object.assign(Error('locked'),{status:423}));await reconnect.timer(10000);assert.equal(reconnect.live.snapshot().enabled,false);assert.equal(reconnect.timers.size,0);
 const app=fs.readFileSync('internal/server/web/app.js','utf8'),begin=app.indexOf('function webSpeakAwait(text)'),end=app.indexOf('\n}',begin)+2;
 let autoStarts=0,autoResumes=0,authorized=true;const syncState={enabled:false,paused:false};const autoContext={window:{aideAssistantLive:{snapshot:()=>syncState,start:withMic=>{assert.equal(withMic,false);autoStarts++;},resume:()=>autoResumes++}},valid:()=>authorized};vm.createContext(autoContext);const syncStart=app.indexOf('window.aideAssistantLiveSync=()=>'),syncEnd=app.indexOf('\n',syncStart);vm.runInContext(app.slice(syncStart,syncEnd),autoContext);autoContext.window.aideAssistantLiveSync();assert.equal(autoStarts,1);syncState.enabled=true;autoContext.window.aideAssistantLiveSync();assert.equal(autoStarts,1,'refresh must not create duplicate monitors');syncState.paused=true;autoContext.window.aideAssistantLiveSync();assert.equal(autoResumes,1);authorized=false;autoContext.window.aideAssistantLiveSync();assert.equal(autoResumes,1,'locked state must not restart coordination');
 let utterances=0;const speechContext={window:{speechSynthesis:{cancel(){},speak(u){utterances++;Promise.resolve().then(()=>u.onend());}}},narration:{active:false,paused:true,cancelled:true},state:{config:{}},ttsSegments:text=>text?[text]:[],pickVoiceForGender:()=>null,SpeechSynthesisUtterance:class{},ttsPlayer:{generation:0,cancelled:false},setInterval:()=>1,clearInterval:()=>{},setTimeout:fn=>fn(),narrSleep:async()=>{}};
 vm.createContext(speechContext);vm.runInContext(app.slice(begin,end),speechContext);await speechContext.webSpeakAwait('继续聆听');assert.equal(utterances,1,'inactive narration state does not freeze voice replies');
 delete speechContext.window.speechSynthesis;await speechContext.webSpeakAwait('no synthesis');
 console.log('PASS continuous assistant: direct voice, reply/resume, echo isolation and voice barge-in, reconnect, short utterance, stop/late permission/late model cancellation, event baseline/dedup, pause and lock boundary');
})().catch(e=>{console.error(e);process.exitCode=1});

// A page refresh must not announce the same persisted blocker again. Only
// opaque event IDs are retained; no private text is written to browser storage.
(async()=>{
 let saved=[],announced=[];
 const hooks={valid:()=>true,watch:async()=>[{id:'persisted-blocker',kind:'task',status:'failed',priority:3,text:'Failed'}],loadSeen:()=>saved,saveSeen:ids=>{saved=ids},events:items=>announced.push(...items),setTimeout:()=>1,clearTimeout:()=>{},state:()=>{},speak:async()=>{}};
 let live=context.window.AideAssistantLive.create(hooks);await live.start(false);await new Promise(resolve=>setImmediate(resolve));live.stop();
 assert.deepEqual(Array.from(saved),['persisted-blocker']);assert.equal(announced.length,1);
 live=context.window.AideAssistantLive.create(hooks);await live.start(false);await new Promise(resolve=>setImmediate(resolve));live.stop();assert.equal(announced.length,1);
 console.log('PASS refresh retains notification dedup without private content');
})().catch(error=>{console.error(error);process.exitCode=1});
