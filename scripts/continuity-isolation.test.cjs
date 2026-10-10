const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const records=new Map();
const crypto=require('node:crypto');
const database={transaction(){const tx={abort(){tx.aborted=true;setTimeout(()=>tx.onabort?.(),0);}};tx.objectStore=()=>({get(key){const req={result:records.get(key)};setTimeout(()=>{req.onsuccess?.();if(!tx.aborted)tx.oncomplete?.();},0);return req;},put(record){records.set(record.key,structuredClone(record));const req={};setTimeout(()=>tx.oncomplete?.(),0);return req;},delete(key){records.delete(key);const req={};setTimeout(()=>tx.oncomplete?.(),0);return req;}});return tx;}};
const context={window:{aideUI:{get:()=>true}},crypto,Blob,Date,JSON,Promise,Error,setTimeout,indexedDB:{open(){const req={result:database};setTimeout(()=>req.onsuccess?.(),0);return req;}}};
vm.runInNewContext(fs.readFileSync('internal/server/web/continuity.js','utf8'),context);
(async()=>{const api=context.window.AideContinuity;
const scopes=['local||','local|recovery-isolation-b|'];
for(const kind of ['chat','scene','file','file-view']){
 await api.write(scopes[0],kind,'same-id',{text:'workspace A',start:12,scrollTop:1800});
 assert.equal(await api.read(scopes[1],kind,'same-id'),null);
 await api.write(scopes[1],kind,'same-id',{text:'workspace B',start:4,scrollTop:80});
 assert.equal((await api.read(scopes[0],kind,'same-id')).text,'workspace A');
 assert.equal((await api.read(scopes[1],kind,'same-id')).text,'workspace B');
 await api.removeMatching(scopes[0],kind,'same-id','stale text');
 assert.equal((await api.read(scopes[0],kind,'same-id')).text,'workspace A');
 await api.removeMatching(scopes[0],kind,'same-id','workspace A');
 assert.equal(await api.read(scopes[0],kind,'same-id'),null);
 assert.equal((await api.read(scopes[1],kind,'same-id')).text,'workspace B');
}
assert.equal(await api.read('', 'file','same-id'),null);
function tab(storage=new Map(),on=true){
 const ctx={...context,window:{aideUI:{get:()=>on}},sessionStorage:{
  getItem:key=>storage.has(key)?storage.get(key):null,
  setItem:(key,value)=>storage.set(key,String(value))
 }};
 vm.runInNewContext(fs.readFileSync('internal/server/web/continuity.js','utf8'),ctx);
 return {api:ctx.window.AideContinuity,storage};
}
const a=tab(),b=tab(),scope='tab-workspace';
const draftA={text:'A unsent',attachments:[{path:'a.md'}],start:3,scrollTop:18};
await a.api.writeTab(scope,'scene','workbench',{session:'A',dir:'alpha'});
await a.api.writeTab(scope,'chat','A',draftA);
// New tabs get the latest shared fallback, then own that snapshot.
assert.equal((await b.api.readTab(scope,'scene','workbench')).session,'A');
await b.api.writeTab(scope,'scene','workbench',{session:'B',dir:'beta'});
await b.api.writeTab(scope,'chat','A',{text:'B divergent draft'});
assert.equal((await tab(a.storage).api.readTab(scope,'scene','workbench')).session,'A');
assert.deepEqual(await tab(a.storage).api.readTab(scope,'chat','A'),draftA);
assert.equal((await tab(b.storage).api.readTab(scope,'scene','workbench')).session,'B');
const clone=tab(new Map(a.storage));
await clone.api.writeTab(scope,'scene','workbench',{session:'clone'});
assert.equal((await a.api.readTab(scope,'scene','workbench')).session,'A');
assert.equal(await a.api.readTab('other-workspace','scene','workbench'),null);
assert.equal(await a.api.readTab(scope,'chat','initial-empty'),null);
await b.api.writeTab(scope,'chat','initial-empty',{text:'other tab'});
assert.equal(await tab(a.storage).api.readTab(scope,'chat','initial-empty'),null);
// A fallback still loading must not overwrite an edit made in this tab.
const racing=tab();
const loading=racing.api.readTab(scope,'chat','race');
const writing=racing.api.writeTab(scope,'chat','race',{text:'new edit'});
assert.equal((await loading).text,'new edit');await writing;
assert.equal((await tab(racing.storage).api.readTab(scope,'chat','race')).text,'new edit');
const off=tab(new Map(),false);
assert.equal(await off.api.writeTab(scope,'chat','A',draftA),false);
assert.equal(await off.api.readTab(scope,'scene','workbench'),null);
assert.equal(off.storage.size,0);
await assert.rejects(a.api.writeTab(scope,'chat','oversize',{text:'x'.repeat(2*1024*1024)}),/2 MiB/);
assert.equal(await a.api.readTab(scope,'chat','oversize'),null);
// Same-file drafts and positions remain independent on reload. Saving or
// discarding A must not delete B's newer shared fallback or tab-local draft.
await a.api.writeTab(scope,'file','same-file',{text:'file A unsaved',hash:'h',start:3});
await b.api.writeTab(scope,'file','same-file',{text:'file B unsaved',hash:'h',start:8});
await a.api.writeTab(scope,'file-view','same-file',{hash:'h',start:3,scrollTop:600});
await b.api.writeTab(scope,'file-view','same-file',{hash:'h',start:8,scrollTop:1200});
assert.equal((await tab(a.storage).api.readTab(scope,'file','same-file')).text,'file A unsaved');
assert.equal((await tab(b.storage).api.readTab(scope,'file','same-file')).text,'file B unsaved');
assert.equal((await tab(a.storage).api.readTab(scope,'file-view','same-file')).scrollTop,600);
assert.equal((await tab(b.storage).api.readTab(scope,'file-view','same-file')).scrollTop,1200);
await a.api.removeMatchingTab(scope,'file','same-file','stale save');
assert.equal((await a.api.readTab(scope,'file','same-file')).text,'file A unsaved');
await a.api.removeMatchingTab(scope,'file','same-file','file A unsaved');
assert.equal(await tab(a.storage).api.readTab(scope,'file','same-file'),null);
assert.equal((await b.api.readTab(scope,'file','same-file')).text,'file B unsaved');
assert.equal((await api.read(scope,'file','same-file')).text,'file B unsaved');
assert.equal((await tab().api.readTab(scope,'file','same-file')).text,'file B unsaved');
const auto=fs.readFileSync('internal/server/web/file-autosave.js','utf8');
assert.ok(auto.includes('AideContinuity.writeTab(')&&auto.includes('AideContinuity.readTab(')&&auto.includes('AideContinuity.savedFileDraft(')&&auto.includes('AideContinuity.writeFileDraft(')&&auto.includes('AideContinuity.listFileDrafts('));
assert.ok(!/AideContinuity\.(write|read|removeMatching)\(/.test(auto));
// Closed contexts preserve divergent file branches without last-writer loss.
await a.api.writeFileDraft(scope,'branch-file',{text:'closed A',hash:'base',start:3});
await b.api.writeFileDraft(scope,'branch-file',{text:'closed B',hash:'base',start:7});
assert.equal((await tab(a.storage).api.listFileDrafts(scope,'branch-file'))[0].text,'closed A');
assert.equal((await tab(b.storage).api.listFileDrafts(scope,'branch-file'))[0].text,'closed B');
const fresh=tab();
assert.deepEqual((await fresh.api.listFileDrafts(scope,'branch-file')).map(d=>d.text).sort(),['closed A','closed B']);
assert.equal((await fresh.api.listFileDrafts('other', 'branch-file')).length,0);
await fresh.api.savedFileDraft(scope,'branch-file','closed A');
assert.deepEqual((await fresh.api.listFileDrafts(scope,'branch-file')).map(d=>d.text),['closed B']);
await fresh.api.dismissFileDraft(scope,'branch-file',(await fresh.api.listFileDrafts(scope,'branch-file'))[0]);
assert.equal((await tab().api.listFileDrafts(scope,'branch-file')).length,0);
for(let i=0;i<32;i++)await tab().api.writeFileDraft(scope,'capacity',{text:'branch '+i,hash:'base'});
await assert.rejects(tab().api.writeFileDraft(scope,'capacity',{text:'overflow',hash:'base'}),/上限/);
assert.equal((await tab().api.listFileDrafts(scope,'capacity')).length,32);
assert.equal((await api.read(scope,'file','capacity')).text,'branch 31');
const byteScope='branch-byte-budget';
for(let i=0;i<7;i++)await tab().api.writeFileDraft(byteScope,'large',{text:String(i)+'x'.repeat(1024*1024),hash:'base'});
await assert.rejects(tab().api.writeFileDraft(byteScope,'large',{text:'8'+'x'.repeat(1024*1024),hash:'base'}),/上限/);
assert.equal((await tab().api.listFileDrafts(byteScope,'large')).length,7);
console.log('Closed-file branches, explicit dismissal, save matching and non-evicting capacity guard PASS');
console.log('Same-file tab drafts, cursor positions and matching cleanup PASS');
console.log('Tab snapshots, reload, opener copy, empty scene, fallback race and size guard PASS');
console.log('Recovery workspace isolation and matching deletion PASS (in-memory IndexedDB fixture)');
})().catch(e=>{console.error(e);process.exitCode=1;});
