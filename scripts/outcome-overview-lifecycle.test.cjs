'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element{constructor(tag){this.tagName=tag;this.children=[];this.isConnected=true;this.textContent='';}append(...items){this.children.push(...items);}replaceChildren(...items){this.children=items;}}
function harness(session){const content=new Element('div'),timers=new Map(),callbacks=[],updates=[];let id=0,calls=0,next=session,blocked=false,deferred;
 const c={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setInterval(fn){timers.set(++id,fn);callbacks.push(fn);return id;},clearInterval(id){timers.delete(id);}};
 vm.runInNewContext(fs.readFileSync(process.argv[2]||'internal/server/web/task-outcome.js','utf8'),c);
 const mounted=c.window.AideTaskOutcome.sessionOverview(content,{session,t:x=>x,selectRun(){},loadOlder(){},blocked:()=>blocked,api:async()=>{calls++;return deferred?await deferred:next;},revision:s=>s.revision,onSession:s=>updates.push(s)});
 return {content,mounted,updates,callbacks,timers,get calls(){return calls},setNext:s=>next=s,setBlocked:v=>blocked=v,setDeferred:p=>deferred=p};}
const completed={id:'s',revision:1,runningRuns:0,runs:[{id:'r',status:'completed',steps:[]}]};
{
 const app=fs.readFileSync('internal/server/web/app.js','utf8'),c={state:{historyLimit:30},sessionPluginPlanView:()=>[]};
 vm.runInNewContext(app.slice(app.indexOf('function sessionRevision('),app.indexOf('function sessionPluginPlanView(')),c);
 assert.notEqual(c.sessionRevision({...completed,hasOlder:true,runs:[],runningRuns:1}),c.sessionRevision({...completed,hasOlder:true,runs:[],runningRuns:0}),'hidden activity metadata participates in session revision');
}
(async()=>{
 for(const session of [completed,{...completed,runs:[]},{...completed,hasOlder:true}])assert.equal(harness(session).timers.size,0,'completed/empty sessions do not poll');
 const running={...completed,revision:2,runningRuns:1,runs:[{id:'r',status:'running',steps:[]}]};
 let h=harness(running);assert.equal(h.timers.size,1);h.setNext({...completed,revision:3});await h.callbacks[0]();assert.equal(h.updates.length,1);assert.equal(h.timers.size,0,'terminal response stops overview polling');await h.callbacks[0]();assert.equal(h.calls,1,'queued callback after stop cannot request');
 h=harness({...completed,hasOlder:true,runningRuns:1});assert.equal(h.timers.size,1,'hidden historical running task keeps polling');h.setNext({...completed,hasOlder:true});await h.callbacks[0]();assert.equal(h.timers.size,0,'hidden historical completion stops polling');
 h=harness({...running,runningRuns:0});assert.equal(h.timers.size,1,'visible running record overrides temporarily stale zero metadata');
 h=harness({...completed,runningRuns:undefined,hasOlder:true});assert.equal(h.timers.size,1,'legacy partial responses remain conservative');
 h=harness(running);let release;h.setDeferred(new Promise(r=>release=r));const pending=h.callbacks[0]();h.mounted.close();release({...completed,revision:4});await pending;assert.equal(h.updates.length,0,'close rejects an in-flight response');assert.equal(h.timers.size,0);await h.callbacks[0]();assert.equal(h.calls,1,'closed callbacks do not fetch');
 h=harness(running);h.setBlocked(true);await h.callbacks[0]();assert.equal(h.calls,0);assert.equal(h.timers.size,0,'blocked/disconnected overview drops its timer');
 console.log('outcome overview lifecycle PASS: terminal/empty idle, hidden running metadata, legacy fallback, stale metadata, in-flight close and blocked timer cleanup');
})().catch(e=>{console.error(e);process.exitCode=1});
