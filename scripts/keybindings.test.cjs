'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/keybindings.js','utf8');
function boot(platform='MacIntel',stored={}){
 let doc={keybindings:stored},listeners=[],subscribers=[],locked=false,modal=false;
 const ctx={navigator:{platform},window:{aideUI:{get:k=>doc[k],set:(k,v)=>{doc[k]=v;subscribers.forEach(f=>f());},subscribe:f=>(subscribers.push(f),()=>{})}},document:{addEventListener:(_,fn)=>listeners.push(fn),querySelector:()=>modal?{}:null,activeElement:{tagName:'BODY'}}};
 vm.runInNewContext(source,ctx);const api=ctx.window.AideKeybindings;
 return {api,ctx,stored:()=>doc.keybindings,lock:()=>locked=true,modal:()=>modal=true,install:actions=>api.install({actions,blocked:()=>locked}),dispatch:e=>listeners[0](e)};
}
function event(key,extras={}){return {key,target:{closest:()=>null},preventDefault(){this.defaultPrevented=true},stopImmediatePropagation(){},...extras};}
for(const platform of ['MacIntel','Linux x86_64']){
 const b=boot(platform),command=platform==='MacIntel'?{metaKey:true}:{ctrlKey:true};
 assert(b.api.matches('search',event('k',command)));assert(b.api.matches('send',event('Enter',{shiftKey:true})));
 assert(!b.api.matches('send',event('Enter',{shiftKey:true,isComposing:true})));
 assert.equal(b.api.set('files','Mod+K').conflict,'搜索会话');
 assert(b.api.set('files','Mod+Q').error);assert(b.api.set('files','G').error);
 assert(!b.api.set('files','Ctrl+Alt+F').error);assert(b.api.matches('files',event('f',{ctrlKey:true,altKey:true})));
 b.api.set('search','');assert(!b.api.matches('search',event('k',command)));
 const reloaded=boot(platform,b.stored());assert(reloaded.api.matches('files',event('f',{ctrlKey:true,altKey:true})));
 assert(!reloaded.api.matches('search',event('k',command)));
 b.api.set('send','Enter');assert(b.api.matches('send',event('Enter')));assert(!b.api.matches('send',event('Enter',{shiftKey:true})));
 b.api.set('send','Ctrl+Enter');assert(b.api.matches('send',event('Enter',{ctrlKey:true})));assert(!b.api.matches('send',event('Enter')));
 let searches=0,newSessions=0;b.api.set('search','Mod+K');b.install({search:()=>searches++,newSession:()=>newSessions++});
 b.dispatch(event('k',command));assert.equal(searches,1);
 b.dispatch(event('k',{...command,repeat:true}));assert.equal(searches,1);
 b.dispatch(event('n',{target:{closest:()=>({})}}));assert.equal(newSessions,0,'typing N never creates a chat');
 b.dispatch(event('n'));assert.equal(newSessions,1);
 b.modal();b.dispatch(event('k',command));assert.equal(searches,1,'modal retains keyboard ownership');
 b.lock();b.dispatch(event('k',command));assert.equal(searches,1,'locked workbench ignores shortcuts');
}
const corrupt=boot('MacIntel',{send:{bad:true},search:'Meta+P',files:'Meta+P',newSession:null});
assert(corrupt.api.matches('send',event('Enter',{shiftKey:true})));assert.equal(corrupt.api.getAll().files,'');assert.equal(corrupt.api.getAll().newSession,'N');
assert(!corrupt.api.eventBinding(event('k',{metaKey:true,keyCode:229})));assert(!corrupt.api.eventBinding(event('k',{ctrlKey:true,getModifierState:()=>true})));
console.log('PASS keybindings: defaults, conflict, reserved keys, persistence, rebind/disable, IME, modal and lock guards on Mac/Linux');
