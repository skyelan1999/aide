'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');

const source=fs.readFileSync('internal/server/web/continuity.js','utf8');
let tick;
const window={aideUI:{get:()=>true},LockCluster:{snapshot:()=>({settled:true,locked:false})},setInterval(fn){tick=fn;return 1;}};
const context={window,document:{visibilityState:'visible'},crypto:{randomUUID:()=> 'test-branch'},
  Blob,Date,JSON,Promise,Error,Set,Map,WeakMap,indexedDB:{open(){}},
};
vm.runInNewContext(source,context);

// The starmap intentionally omits recovery-transfer.js. Its minute timer must
// be harmless when the optional retention API is not installed.
assert.doesNotThrow(()=>tick());

let called=0;
window.AideContinuity.pruneExpiredRecoveryOperations=async()=>{called++;};
tick();
setImmediate(()=>{
  assert.equal(called,1,'retention cleanup still runs when its module is installed');
  console.log('Optional continuity retention API guard PASS');
});
