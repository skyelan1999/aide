'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element{constructor(tag){this.tagName=tag;this.children=[];this.isConnected=true;this.textContent='';}append(...items){this.children.push(...items);}replaceChildren(...items){this.children=items;}}
async function harness(session,options={}){const content=new Element('div'),timers=new Map(),callbacks=[],updates=[];let id=0,calls=0,next=options.next||session,blocked=false,deferred;
 const c={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setInterval(fn){timers.set(++id,fn);callbacks.push(fn);return id;},clearInterval(id){timers.delete(id);}};
 vm.runInNewContext(fs.readFileSync(process.argv[2]||'internal/server/web/task-outcome.js','utf8'),c);
 const mounted=c.window.AideTaskOutcome.sessionOverview(content,{session,t:x=>x,selectRun(){},loadOlder(){},blocked:()=>blocked,api:async()=>{calls++;return deferred?await deferred:next;},revision:s=>s.revision,onSession:s=>updates.push(s)});
 await mounted.ready;calls=0;
 return {content,mounted,updates,callbacks,timers,refresh:id=>c.window.AideTaskOutcome.refreshSession?.(id),module:c.window.AideTaskOutcome,get calls(){return calls},setNext:s=>next=s,setBlocked:v=>blocked=v,setDeferred:p=>deferred=p};}
const completed={id:'s',revision:1,runningRuns:0,runs:[{id:'r',status:'completed',steps:[]}]};
{
 const app=fs.readFileSync('internal/server/web/app.js','utf8'),c={state:{historyLimit:30},sessionPluginPlanView:()=>[]};
 vm.runInNewContext(app.slice(app.indexOf('function sessionRevision('),app.indexOf('function sessionPluginPlanView(')),c);
 assert.notEqual(c.sessionRevision({...completed,hasOlder:true,runs:[],runningRuns:1}),c.sessionRevision({...completed,hasOlder:true,runs:[],runningRuns:0}),'hidden activity metadata participates in session revision');
}
{
 const app=fs.readFileSync('internal/server/web/app.js','utf8'),events={},refreshes=[];let sidebars=0,approvals=0;
 const c={sessionsEv:null,state:{token:'local-test-not-a-secret'},EventSource:function(){this.addEventListener=(name,fn)=>events[name]=fn;},scheduleSessionsRefresh:()=>sidebars++,refreshApprovalPolicy:async()=>{approvals++;},window:{AideManagement:{refresh(){}},AideTaskOutcome:{refreshSession:id=>refreshes.push(id)}}};
 vm.runInNewContext(app.slice(app.indexOf('function setupGlobalEvents()'),app.indexOf('/* ── 小秘系统会话密码门')),c);c.setupGlobalEvents();events['sessions-changed']({data:JSON.stringify({sessionId:'s'})});events['sessions-changed']({data:'broken'});events['open']();assert.equal(sidebars,2);assert.deepEqual(refreshes,['s',undefined]);assert.equal(approvals,1,'reconnect keeps approval refresh');
}
(async()=>{
 for(const session of [completed,{...completed,runs:[]},{...completed,hasOlder:true}])assert.equal((await harness(session)).timers.size,0,'completed/empty sessions do not poll');
 let idle=await harness(completed);idle.setNext({...completed,revision:2,runningRuns:1,hasOlder:true,runsTotal:64});await idle.refresh('s');assert.equal(idle.updates.length,1,'global event discovers external activity from terminal overview');idle.mounted.close();
 // A panel opened after external changes must not wait for another event.
 let initial=await harness(completed,{next:{...completed,revision:2,runningRuns:1,hasOlder:true,runsTotal:64}});assert.equal(initial.updates.length,1);assert.equal(initial.timers.size,1);initial.mounted.close();
 let owned=await harness(completed);owned.setNext({...completed,revision:2});await owned.refresh('different-session');assert.equal(owned.calls,0,'unrelated session does not refresh');await owned.refresh();assert.equal(owned.calls,1,'reconnect refreshes current overview once');assert.equal(owned.timers.size,0,'terminal reconnect remains idle');owned.mounted.close();await owned.refresh('s');assert.equal(owned.calls,1,'closed idle overview ignores global event');
 owned=await harness(completed);let resolveOld;owned.setDeferred(new Promise(r=>resolveOld=r));const first=owned.refresh('s');await owned.refresh('s');await owned.refresh('s');owned.setDeferred(null);owned.setNext({...completed,revision:3});resolveOld({...completed,revision:2});await first;await Promise.resolve();assert.equal(owned.calls,2,'events during a read coalesce to one trailing authoritative read');assert.equal(owned.updates.at(-1).revision,3);owned.mounted.close();
 owned=await harness(completed);let resolveClosed;owned.setDeferred(new Promise(r=>resolveClosed=r));const closing=owned.refresh('s');await owned.refresh('s');owned.mounted.close();resolveClosed({...completed,revision:2});await closing;assert.equal(owned.updates.length,0);assert.equal(owned.calls,1,'close discards trailing refresh and in-flight result');
 owned=await harness(completed);owned.setNext({...completed,id:'other',revision:2});await owned.refresh('s');assert.equal(owned.updates.length,0,'wrong session response cannot replace overview');owned.setBlocked(true);await owned.refresh('s');assert.equal(owned.calls,1,'locked or switched view cannot fetch');
 const running={...completed,revision:2,runningRuns:1,runs:[{id:'r',status:'running',steps:[]}]};
 let h=await harness(running);assert.equal(h.timers.size,1);h.setNext({...completed,revision:3});await h.callbacks[0]();assert.equal(h.updates.length,1);assert.equal(h.timers.size,0,'terminal response stops overview polling');await h.callbacks[0]();assert.equal(h.calls,1,'queued callback after stop cannot request');
 h=await harness({...completed,hasOlder:true,runningRuns:1});assert.equal(h.timers.size,1,'hidden historical running task keeps polling');h.setNext({...completed,hasOlder:true});await h.callbacks[0]();assert.equal(h.timers.size,0,'hidden historical completion stops polling');
 h=await harness({...running,runningRuns:0});assert.equal(h.timers.size,1,'visible running record overrides temporarily stale zero metadata');
 h=await harness({...completed,runningRuns:undefined,hasOlder:true});assert.equal(h.timers.size,1,'legacy partial responses remain conservative');
 h=await harness(running);let release;h.setDeferred(new Promise(r=>release=r));const pending=h.callbacks[0]();h.mounted.close();release({...completed,revision:4});await pending;assert.equal(h.updates.length,0,'close rejects an in-flight response');assert.equal(h.timers.size,0);await h.callbacks[0]();assert.equal(h.calls,1,'closed callbacks do not fetch');
 h=await harness(running);let finishSlow;h.setDeferred(new Promise(r=>finishSlow=r));const slow=h.callbacks[0]();await h.callbacks[0]();h.setDeferred(null);finishSlow({...completed,revision:3});await slow;await Promise.resolve();assert.equal(h.calls,1,'overlapping interval ticks do not queue a redundant read');h.mounted.close();
 h=await harness(running);h.setBlocked(true);await h.callbacks[0]();assert.equal(h.calls,0);assert.equal(h.timers.size,0,'blocked/disconnected overview drops its timer');
 console.log('outcome overview lifecycle PASS: initial authoritative read, external event/reconnect, scoped identity, event coalescing, idle close, wrong-session and blocked boundaries; terminal/empty idle, hidden running metadata, legacy fallback, stale metadata, in-flight close and blocked timer cleanup');
})().catch(e=>{console.error(e);process.exitCode=1});
