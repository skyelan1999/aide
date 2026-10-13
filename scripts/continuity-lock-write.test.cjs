'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');

let transaction,openRequest;
const records=new Map(),tabStorage=new Map();
const db={createObjectStore(){},transaction(_store,mode){
  const tx={mode,aborted:false,events:{},addEventListener(name,fn){(this.events[name]??=[]).push(fn);},abort(){if(this.aborted)return;this.aborted=true;for(const fn of this.events.abort||[])fn();this.onabort?.();},objectStore(){return {
    get(key){const req={};tx.getKey=key;transactionGet=req;return req;},
    put(record){tx.pending=record;},
    delete(key){tx.pendingDelete=key;}
  };}};
  transaction=tx;return tx;
}};
let transactionGet;
const indexedDB={open(){openRequest={};queueMicrotask(()=>{openRequest.result=db;openRequest.onupgradeneeded?.();openRequest.onsuccess?.();});return openRequest;}};
const context={indexedDB,crypto:{randomUUID:()=>`id-${Math.random()}`},Blob,Date,JSON,Map,Promise,Error,Set,
  document:{visibilityState:'visible'},navigator:{},sessionStorage:{getItem:k=>tabStorage.get(k)??null,setItem:(k,v)=>tabStorage.set(k,v),removeItem:k=>tabStorage.delete(k)},
  window:{aideUI:{get:()=>true},setInterval:()=>0},setTimeout,clearTimeout,console};
vm.runInNewContext(fs.readFileSync('internal/server/web/continuity.js','utf8'),context);
const api=context.window.AideContinuity;

(async()=>{
  let active=true;
  const pending=api.writeTab('workspace-A','chat','session-1',{text:'recover me'},{active:()=>active});
  await new Promise(resolve=>setImmediate(resolve));
  assert.ok(transaction,'chat recovery opened an IndexedDB transaction');
  assert.equal(tabStorage.size,1,'same-tab recovery snapshot is synchronously preserved');
  active=false;api.abortPendingWrites();
  await assert.rejects(pending,/interrupted|事务中断|恢复写入已取消/i);
  assert.equal(transaction.aborted,true,'locking aborts the pending IndexedDB transaction');
  assert.equal(records.size,0,'aborted recovery write did not commit');

  // A stale caller must not create another snapshot or transaction after lock.
  const before=tabStorage.size;const blocked=await api.writeTab('workspace-A','scene','workbench',{session:'session-1'},{active:()=>false});
  assert.equal(blocked,false);assert.equal(tabStorage.size,before);assert.equal(records.size,0);
  const source=fs.readFileSync('internal/server/web/app.js','utf8');
  assert.match(source,/function cancelPendingRecoveryWrites\(\)\{chatRecoveryEpoch\+\+;clearTimeout\(recoveryTimer\);window\.AideContinuity\?\.abortPendingWrites\?\.\(\);\}/);
  assert.match(source,/cancelPendingRecoveryWrites\(\);\s*lockScreen\.locked = true/);
  assert.match(source,/function lockScreenNow\(\)\s*\{[\s\S]*?cancelPendingRecoveryWrites\(\);/);
  assert.match(source,/function persistWorkspaceScene\(overrides=\{\}\)\{[\s\S]*?writeTab\(scope,'scene','workbench',scene,\{active\}\)/);
  const map=fs.readFileSync('internal/server/web/starmap.js','utf8');
  assert.match(map,/function persistSkyView\(active=\(\)=>!locked\)/);
  assert.match(map,/writeTab\(scope,'scene','starmap',value,\{active:\(\)=>active\(\)&&!locked/);
  assert.match(map,/lockStateKnown=true;locked=value;if\(value\)AideContinuity\.abortPendingWrites\?\.\(\);/);
  console.log('Lock-time draft write cancellation, retained tab snapshot and stale-scene guard PASS');
})().catch(error=>{console.error(error);process.exitCode=1;});
