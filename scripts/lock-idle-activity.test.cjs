'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {environment}=require('./lock-cluster-state.test.js');
const app=require('./fixtures/app-source.cjs').readAppSource();
const timerCode=app.slice(app.indexOf('const lockScreen ='),app.indexOf('function refreshLockStatus()'));
const inputCode=app.slice(app.indexOf('const lockActivityEvents ='),app.indexOf('setInterval(() => { if (lockScreen.locked) refreshLockStatus();'));
const remoteHook=app.match(/LockCluster\.setRemoteActivityHook\([^\n]+/)[0];
function install(tab,seconds=2){
 const c=tab.context,handlers={},queued=[];let locks=0,leases=0;
 const add=c.window.addEventListener,set=c.setTimeout;
 c.window.addEventListener=(name,fn,options)=>{(handlers[name]??=[]).push({fn,options});add(name,fn,options);};
 c.setTimeout=(fn,ms)=>{queued.push(fn);return set(fn,ms);};
 c.state={config:{hasPassword:true,lockTimeoutSec:seconds}};
 c.LockCluster=tab.lc;c.window.AideLANAccess={activity(){leases++;}};
 c.lockScreenNow=()=>{locks++;void tab.lc.requestLock('idle');};
 vm.runInContext(timerCode+'\n'+remoteHook+'\n'+inputCode,c);
 tab.lc.on('effective',locked=>{c.fixtureLocked=locked;vm.runInContext('lockScreen.locked=fixtureLocked; resetIdleTimer();',c);});
 tab.lc.on('role',()=>vm.runInContext('resetIdleTimer()',c));
 vm.runInContext('resetIdleTimer()',c);
 return {handlers,queued,locks:()=>locks,leases:()=>leases,activity(type){
  const list=handlers[type]||[];assert.ok(list.length,`missing ${type} activity`);
  for(const {fn,options}of list){assert.equal(options.capture,true,'nested viewer can stop bubbling');assert.equal(options.passive,true);fn({type,defaultPrevented:true});}
 }};
}
(async()=>{
 // Exercise the actual cluster BroadcastChannel ping and actual app deadline code.
 for(const type of ['wheel','pointerdown','pointermove','pointerup','scroll','keydown']){
  const e=environment(),main=e.tab('/');await e.advance(1800);const file=e.tab('/starmap.html');await e.advance(1800);
  const m=install(main),f=install(file);
  for(let i=0;i<6;i++){await e.advance(900);f.activity(type);await e.flush();}
  assert.equal(e.server.locked,false,`${type}: file-view interaction did not renew master`);
  assert.equal(m.locks(),0);assert.ok(f.leases()>0,'LAN activity was not renewed');
  await e.advance(1999);assert.equal(e.server.locked,false,'locked before the configured idle timeout');
  await e.advance(2);assert.equal(e.server.locked,true,'genuine idle stopped locking');assert.equal(m.locks(),1);assert.equal(file.lc.effectiveLocked(),true);
  f.activity(type);await e.advance(100);assert.equal(e.server.locked,true,'activity unlocked an already locked page');
  main.close();file.close();
 }
 const e=environment(),tab=e.tab('/');await e.advance(1800);const m=install(tab);
 const obsolete=m.queued.at(-1);await e.advance(1500);m.activity('pointerdown');await e.advance(501);
 obsolete();await e.flush();assert.equal(e.server.locked,false,'a queued obsolete callback ignored renewed activity');
 await tab.lc.requestLock('manual');await e.flush();m.activity('wheel');await e.advance(2500);assert.equal(e.server.locked,true,'manual lock was bypassed');
 const off=environment(),unlocked=off.tab('/');await off.advance(1800);install(unlocked,0);await off.advance(10000);assert.equal(off.server.locked,false,'disabled idle lock was re-enabled');
 console.log('PASS captured pointer/wheel/nested-scroll activity, real file→master pings, deadline renewal, obsolete callback, genuine idle and manual-lock boundaries');
})().catch(e=>{console.error(e);process.exitCode=1;});
