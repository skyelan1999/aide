#!/usr/bin/env node
'use strict';
// Multi-tab regression using the shipped cluster script, fake time and an isolated
// authoritative server. No production credentials, sessions or paid models.
const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');
const source=fs.readFileSync(process.argv[2]||'internal/server/web/lock-cluster.js','utf8');
function environment(){
 let now=0,seq=0,tabSeq=0,timers=new Map(),channels=[];
 const server={locked:false,gen:0,failPut:false,failGet:false,delayGet:0};
 function schedule(fn,ms,interval=0,owner){const id=++seq;timers.set(id,{fn,time:now+ms,interval,owner});return id;}
 async function flush(){for(let i=0;i<20;i++)await Promise.resolve();}
 async function advance(ms){const end=now+ms;await flush();let count=0;while(true){const tasks=[...timers].filter(([,t])=>t.time<=end).sort((a,b)=>a[1].time-b[1].time||a[0]-b[0]);if(!tasks.length)break;const [id,t]=tasks[0];if(++count>10000)throw Error('timer loop');now=t.time;if(t.interval) t.time+=t.interval;else timers.delete(id);t.fn();await flush();}now=end;await flush();}
 function tab(path='/'){
  const id='tab-'+(++tabSeq),events={},docEvents={},stats={asserts:0};
  const on=(store,name,fn)=>(store[name]??=[]).push(fn);
  const w={addEventListener:(name,fn)=>on(events,name,fn)};
  const document={hidden:false,addEventListener:(name,fn)=>on(docEvents,name,fn)};
  const context={window:w,document,location:{pathname:path,hash:''},localStorage:{getItem:()=> 'test-token'},crypto:{randomUUID:()=>id},AbortController,console,
   Date:class extends Date{static now(){return now;}},
   setTimeout:(fn,ms)=>schedule(fn,ms,0,id),setInterval:(fn,ms)=>schedule(fn,ms,ms,id),
   clearTimeout:x=>timers.delete(x),clearInterval:x=>timers.delete(x),
   fetch:async (url,options={})=>{
    if(options.method!=='PUT'&&server.failGet)return {ok:false,status:503,json:async()=>({})};
    if(options.method==='PUT'){
     if(server.failPut)return {ok:false,status:500,json:async()=>({error:'disk full'})};
     server.locked=JSON.parse(options.body).locked;server.gen++;
    }
    const snapshot={locked:server.locked,gen:server.gen};
    if(options.method!=='PUT'&&server.delayGet)await new Promise(resolve=>schedule(resolve,server.delayGet,0,id));
    return {ok:true,json:async()=>snapshot};
   },
   BroadcastChannel:class{
    constructor(){channels.push(this);this.owner=id;}
    postMessage(message){if(message.type==='assert')stats.asserts++;for(const c of channels)if(c!==this)schedule(()=>c.onmessage?.({data:structuredClone(message)}),0,0,c.owner);}
    close(){channels=channels.filter(c=>c!==this);}
   }
  };
  vm.createContext(context);vm.runInContext(source,context);
  return {lc:w.LockCluster,stats,events,receive(message){const c=channels.find(c=>c.owner===id);c.onmessage({data:structuredClone(message)});},close(){for(const fn of events.beforeunload||[])fn();for(const fn of events.pagehide||[])fn();for(const [key,t]of timers)if(t.owner===id)timers.delete(key);channels=channels.filter(c=>c.owner!==id);}};
 }
 return {server,tab,advance,flush};
}
(async()=>{
 const e=environment(),file=e.tab('/starmap.html');await e.advance(1800);assert(file.lc.isMaster());
 const main=e.tab('/');await e.advance(1800);assert(main.lc.isMaster());assert(file.lc.isSlave());
 const oldHeartbeats=file.stats.asserts;await e.advance(4500);assert.equal(file.stats.asserts,oldHeartbeats,'demoted tab kept sending master assertions');
 await main.lc.requestLock();await e.advance(30);assert(main.lc.effectiveLocked());assert(file.lc.effectiveLocked(),'map did not follow lock');
 const slaveScope=await file.lc.prepareUnlock();assert.equal(slaveScope.global,false);await file.lc.handleUnlockSuccess(slaveScope);assert.equal(file.lc.effectiveLocked(),false);assert.equal(e.server.locked,true);
 e.tab('/');await e.advance(1200);assert.equal(file.lc.effectiveLocked(),false,'welcome for a new tab relocked a locally unlocked slave');
 const scope=await main.lc.prepareUnlock();assert(scope.global);e.server.locked=false;e.server.gen++;await main.lc.handleUnlockSuccess(scope);await e.advance(30);assert.equal(file.lc.effectiveLocked(),false);
 main.close();await e.advance(100);const refreshed=e.tab('/');await e.advance(2000);assert.equal(refreshed.lc.effectiveLocked(),false,'refresh resurrected old lock');
 // A rejected write must reject the caller, keep the visual lock, and leave authority intact.
 e.server.failPut=true;await assert.rejects(refreshed.lc.requestLock());assert.equal(e.server.locked,false);assert.equal(refreshed.lc.effectiveLocked(),true);
 e.server.failPut=false;await refreshed.lc.requestLock();await e.advance(30);
 const oldScope=await file.lc.prepareUnlock();await refreshed.lc.requestLock();await e.advance(30);await assert.rejects(file.lc.handleUnlockSuccess(oldScope));
 // Background-tab / lost-channel recovery observes a server-only unlock without a page reload.
 e.server.locked=false;e.server.gen++;await e.advance(5500);assert.equal(file.lc.effectiveLocked(),false);assert.equal(refreshed.lc.effectiveLocked(),false);
 // A slow GET captured before locking must not resurrect the earlier unlocked generation.
 e.server.delayGet=4000;const late=file.lc.prepareUnlock();await e.flush();await refreshed.lc.requestLock();await e.advance(4500);await late.catch(()=>{});assert.equal(file.lc.effectiveLocked(),true);
 // A late joiner must keep its neutral startup veil while a peer advertises an
 // old persisted lock snapshot; only the current server generation may settle it.
 const b=environment(),master=b.tab('/');await b.advance(1800);await master.lc.requestLock();
 b.server.locked=false;b.server.gen++;b.server.delayGet=500;
 const joining=b.tab('/starmap.html'),effective=[];joining.lc.on('effective',value=>effective.push(value));
 let ready=false;joining.lc.onReady().then(()=>{ready=true;});
 await b.advance(100);assert.equal(ready,false,'peer broadcast settled startup before the server read');assert.deepEqual(effective,[],'stale peer lock escaped the neutral startup veil');
 await b.advance(500);assert.equal(ready,true);assert.equal(joining.lc.effectiveLocked(),false);assert.deepEqual(effective,[false]);
 const c=environment();c.server.failGet=true;const unavailable=c.tab('/');let fallbackReady=false; c.tab('/starmap.html').lc.onReady().then(()=>{fallbackReady=true;});
 await c.advance(2400);assert.equal(fallbackReady,false,'unavailable server should preserve the bounded neutral join');
 await c.advance(200);assert.equal(fallbackReady,true,'unavailable server must settle at the hard timeout');
 // Identity commits the authoritative unlock. A failed secondary GET is not a
 // generation conflict and must not strand the UI behind the password veil.
 const d=environment(),primary=d.tab('/');await d.advance(1800);await primary.lc.requestLock();
 const verifiedScope=await primary.lc.prepareUnlock();d.server.locked=false;d.server.gen++;
 const receipt={locked:false,gen:d.server.gen};d.server.failGet=true;
 await primary.lc.handleUnlockSuccess(verifiedScope,receipt);
 assert.equal(primary.lc.effectiveLocked(),false,'successful verification stranded by failed follow-up GET');
 d.server.failGet=false;const secondary=d.tab('/starmap.html');await d.advance(1800);await primary.lc.requestLock();await d.advance(30);
 const localScope=await secondary.lc.prepareUnlock();d.server.failGet=true;
 await secondary.lc.handleUnlockSuccess(localScope,{locked:true,gen:d.server.gen});
 assert.equal(secondary.lc.effectiveLocked(),false);assert.equal(d.server.locked,true,'local identity receipt must not globally unlock');
 d.server.failGet=false;await primary.lc.requestLock();await d.advance(30);
 const stale=await primary.lc.prepareUnlock();d.server.locked=false;d.server.gen++;
 const oldReceipt={locked:false,gen:d.server.gen};await primary.lc.requestLock();
 await assert.rejects(primary.lc.handleUnlockSuccess(stale,oldReceipt));assert.equal(primary.lc.effectiveLocked(),true,'new lock defeated by prior verified receipt');
 await assert.rejects(primary.lc.handleUnlockSuccess(stale,{locked:false,gen:stale.gen+9}),'receipt must match the identity request generation');
 // A sleeping/closed leader can disappear during password verification. The
 // server verified this exact generation; local dismissal must survive election.
 const r=environment(),leader=r.tab('/');await r.advance(1800);
 const follower=r.tab('/');await r.advance(1800);await leader.lc.requestLock();await r.advance(30);
 const duringElection=await follower.lc.prepareUnlock();assert.equal(duringElection.global,false);
 leader.close();await r.advance(1800);assert(follower.lc.isMaster());
 await follower.lc.handleUnlockSuccess(duringElection,{locked:true,gen:r.server.gen});
 assert.equal(follower.lc.effectiveLocked(),false,'valid local receipt rejected after leader election');
 assert.equal(r.server.locked,true,'local receipt cleared global authority');
 await r.advance(5500);assert.equal(follower.lc.effectiveLocked(),false,'same-generation poll lost verified dismissal');
 await follower.lc.requestLock();assert.equal(follower.lc.effectiveLocked(),true,'new manual lock must revoke local dismissal');
 const f=environment(),failedMaster=f.tab('/');await f.advance(1800);const failedSlave=f.tab('/starmap.html');await f.advance(1800);
 f.server.failPut=true;await assert.rejects(failedMaster.lc.requestLock());await f.advance(30);
 const failedScope=await failedSlave.lc.prepareUnlock();await failedSlave.lc.handleUnlockSuccess(failedScope,{locked:false,gen:f.server.gen});
 assert.equal(failedSlave.lc.effectiveLocked(),false,'valid identity receipt did not dismiss a failed peer lock');
 await f.advance(5500);assert.equal(failedSlave.lc.effectiveLocked(),false,'stale peer heartbeat resurrected a verified veil');
 failedMaster.close();await f.advance(1800);assert(failedSlave.lc.isMaster());
 const revivedMaster=f.tab('/');await f.advance(1800);assert(failedSlave.lc.isSlave());assert.equal(failedSlave.lc.effectiveLocked(),false,'election dropped a verified dismissal at an unlocked authority generation');
 f.server.failPut=false;await revivedMaster.lc.requestLock();await f.advance(30);assert.equal(failedSlave.lc.effectiveLocked(),true,'new persisted lock must revoke verified dismissal');
 // Lifecycle restoration must restart an election canceled before initial readiness.
 const lifecycle=environment(),returning=lifecycle.tab('/');let restored=false;returning.lc.onReady().then(()=>restored=true);for(const fn of returning.events.pagehide||[])fn();await lifecycle.advance(4000);for(const fn of returning.events.pageshow||[])fn();await lifecycle.advance(2600);assert.equal(restored,true,'pagehide during joining left a permanently unresolved veil');
 // Unchanged heartbeats are not lock/unlock transitions (nor resume commands).
 const transitions=environment(),tm=transitions.tab('/');await transitions.advance(1800);const ts=transitions.tab('/starmap.html');await transitions.advance(1800);const seen=[];ts.lc.on('effective',value=>seen.push(value));await transitions.advance(6000);assert.deepEqual(seen,[],'unchanged master heartbeat re-emitted unlock transitions');
 // The peer's election/visual boolean cannot contradict its server authority receipt.
 ts.receive({type:'assert',from:tm.lc.tabId,pri:[0,0,tm.lc.tabId],gen:999,masterLocked:true,server:{locked:false,gen:transitions.server.gen}});assert.equal(ts.lc.effectiveLocked(),false,'stale peer visual state resurrected an unlocked authority');
 await tm.lc.requestLock();await transitions.advance(30);ts.receive({type:'assert',from:tm.lc.tabId,pri:[0,0,tm.lc.tabId],gen:1000,masterLocked:false,server:{locked:true,gen:transitions.server.gen}});assert.equal(ts.lc.effectiveLocked(),true,'peer visual state bypassed a persisted server lock');
 // A failed newer lock write still revokes an earlier identity continuation locally.
 const intentEnv=environment(),intentTab=intentEnv.tab('/');await intentEnv.advance(1800);await intentTab.lc.requestLock();const priorIntent=await intentTab.lc.prepareUnlock();intentEnv.server.locked=false;intentEnv.server.gen++;const committed={locked:false,gen:intentEnv.server.gen};intentEnv.server.failPut=true;await assert.rejects(intentTab.lc.requestLock());await assert.rejects(intentTab.lc.handleUnlockSuccess(priorIntent,committed));assert.equal(intentTab.lc.effectiveLocked(),true,'old identity continuation cleared a newer failed lock intent');
 // The app continuation must not hide a new lock arriving across its await.
 const app=fs.readFileSync('internal/server/web/app.js','utf8');
 const dismiss=app.match(/async function dismissAfterUnlock\([^]*?\n}/)[0],veil={hidden:false,classList:{remove(){}}};
 const lc={handleUnlockSuccess:async()=>{},effectiveLocked:()=>true};
 const view={window:{LockCluster:lc},LockCluster:lc,lockScreen:{locked:true},Date,t:s=>s,$:()=>veil,state:{config:{}},voiceAssistantDisplayName:()=>'',toast(){},speakReply(){},resetIdleTimer(){}};
 vm.createContext(view);vm.runInContext(dismiss,view);
 await assert.rejects(view.dismissAfterUnlock({global:true,gen:0},{locked:false,gen:1}),/锁屏状态已变化/);
 assert.equal(veil.hidden,false,'old unlock continuation hid a newer lock');
 console.log('PASS: master demotion, map sync, local dismissal, new-tab welcome, unlock then refresh, write failure, stale unlock, server recovery, out-of-order GET');
})().catch(error=>{console.error(error);process.exitCode=1;});
