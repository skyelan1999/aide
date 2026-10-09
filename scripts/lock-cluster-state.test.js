#!/usr/bin/env node
'use strict';
// Multi-tab regression using the shipped cluster script, fake time and an isolated
// authoritative server. No production credentials, sessions or paid models.
const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/lock-cluster.js','utf8');
function environment(){
 let now=0,seq=0,tabSeq=0,timers=new Map(),channels=[];
 const server={locked:false,gen:0,failPut:false,delayGet:0};
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
  return {lc:w.LockCluster,stats,events,close(){for(const fn of events.beforeunload||[])fn();for(const fn of events.pagehide||[])fn();for(const [key,t]of timers)if(t.owner===id)timers.delete(key);channels=channels.filter(c=>c.owner!==id);}};
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
 console.log('PASS: master demotion, map sync, local dismissal, new-tab welcome, unlock then refresh, write failure, stale unlock, server recovery, out-of-order GET');
})().catch(error=>{console.error(error);process.exitCode=1;});
