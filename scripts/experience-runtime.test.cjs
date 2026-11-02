'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/experience.js','utf8');
for(const mode of ['normal','hidden','reduced']){
 const root={dataset:{motion:mode==='reduced'?'reduced':'auto'},toggleAttribute(){}};
 const body={classList:{contains:()=>false},animate:null};
 const context={document:{documentElement:root,body,hidden:mode==='hidden',getElementById:()=>null,querySelector:()=>null,addEventListener(){},removeEventListener(){}},location:{pathname:'/',hash:''},sessionStorage:{getItem:()=> '1'},MutationObserver:class{observe(){}disconnect(){}},getComputedStyle:()=>({gridTemplateColumns:'100px 100px'}),innerWidth:1440,setTimeout,clearTimeout,cancelAnimationFrame(){},requestAnimationFrame:()=>1};
 context.window=context;context.addEventListener=()=>{};context.matchMedia=()=>({matches:false,addEventListener(){}});
 vm.createContext(context);vm.runInContext(source,context,{filename:'experience.js'});
 const api=context.aideExperience;
 assert(api,mode+': complete script creates the API');assert.equal(root.dataset.motionLayer,'ready');
 for(const name of ['leave','settle','reopen','changePanels'])assert.equal(typeof api[name],'function',name);
 assert.equal(api.replay,undefined,'removed replay API stays removed');
 let closed=0,changed=0;
 const finish=api.leave(null,()=>closed++);finish();api.settle(null);api.reopen(null);api.changePanels(()=>changed++);
 assert.equal(closed,1);assert.equal(changed,1,'panel mutation is immediate');
 context.aideExperience=null;vm.runInContext(source,context);assert.equal(typeof context.aideExperience.leave,'function','null API initializes');context.aideExperience=api;
 api.extension='retained';vm.runInContext(source,context);assert.equal(context.aideExperience,api,'preserve existing API identity');assert.equal(api.extension,'retained');
}
console.log('PASS complete experience runtime: normal, hidden, reduced; public callbacks and API identity');
