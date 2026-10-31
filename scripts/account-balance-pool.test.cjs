'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {amounts,gauge,preference}=require('../internal/server/web/account-balance-pool.js');
test('only finite supported amounts become balances; unknown is not zero',()=>{
 assert.deepEqual(amounts({balance_infos:[{currency:'CNY',total_balance:'0'},{currency:'USD',total_balance:'-2'},{currency:'CNY',total_balance:''},{currency:'CNY',total_balance:null},{currency:'USD',total_balance:'Infinity'},{currency:'EUR',total_balance:'100'}]}),[{currency:'CNY',value:0},{currency:'USD',value:-2}]);
 assert.deepEqual(amounts({}),[]);
});
test('alarm boundary, negative balances, scale clamping and persisted preference validation',()=>{
 assert.equal(gauge(10,10,100).low,true);assert.equal(gauge(10.01,10,100).low,false);
 assert.deepEqual(gauge(-1,10,100),{level:0,line:10,low:true,empty:true});
 assert.equal(gauge(200,10,100).level,100);
 assert.deepEqual(preference({threshold:-1,ceiling:Infinity},'USD'),{threshold:2,ceiling:20});
 assert.deepEqual(preference({threshold:0,ceiling:50},'CNY'),{threshold:0,ceiling:50});
});
const flush=async()=>{for(let i=0;i<5;i++)await new Promise(setImmediate);};
function fixture(api,{count=2,active=()=>true}={}){
 const timers=[],observers=[],store=new Map();
 class Element{
  constructor(tag){this.tagName=tag;this.children=[];this.attributes={};this.style={};this.dataset={};this.isConnected=true;this.classList={add(){},remove(){}};}
  append(...nodes){this.children.push(...nodes);}
  replaceChildren(...nodes){this.children=nodes;}
  setAttribute(k,v){this.attributes[k]=v;}
  querySelectorAll(tag){return this.children.flatMap(n=>[...(n.tagName===tag?[n]:[]),...n.querySelectorAll(tag)]);}
 }
 const sheet={open:true,classList:{contains:()=>sheet.open}};
 const document={getElementById:id=>id==='settings-sheet'?sheet:null,visibilityState:'visible',documentElement:{},createElement:tag=>new Element(tag)};
 const context={window:{document,localStorage:{getItem:k=>store.get(k)||null,setItem:(k,v)=>store.set(k,v)}},AbortController,URLSearchParams,Date,console,MutationObserver:class{constructor(fn){observers.push(fn);}observe(){}disconnect(){}},setTimeout:fn=>(timers.push(fn),timers.length),clearTimeout(){}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/account-balance-pool.js','utf8'),context);
 const t=(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>String(args[n]));
 const pool=context.window.AideAccountBalancePool.create({api,t,active,scope:()=> 'fixture',config:{modelProviders:[{id:'provider',name:'Provider',keys:Array.from({length:count},(_,i)=>({id:String(i),label:'Key '+i}))}]}});
 return{pool,timers,observers,store,sheet,all:tag=>pool.querySelectorAll(tag),cards:()=>pool.querySelectorAll('article'),start:async()=>{timers.shift()();await flush();}};
}
test('all keys have separate cards and alarms; failures retain stale data without reflecting errors',async()=>{
 let fail=false;const queries=[];
 const f=fixture(async(path)=>{queries.push(path);if(fail)throw Error('sensitive-upstream-message');const id=new URLSearchParams(path.split('?')[1]).get('keyId');return{supported:true,providerId:'provider',keyId:id,balanceScope:'account',balance_infos:[{currency:'CNY',total_balance:id==='0'?'5':'50'}]};});
 await f.start();assert.equal(f.cards().length,2);assert.equal(f.cards()[0].dataset.state,'low');assert.equal(f.cards()[1].dataset.state,'ready');
 const threshold=f.all('input').find(x=>x.attributes['aria-label']?.includes('Key 0 / CNY / 报警线'));threshold.value='1';await f.all('button').find(x=>x.attributes['aria-label']==='Provider / Key 0 / CNY / 应用报警设置').onclick();assert.equal(f.cards()[0].dataset.state,'ready');assert.equal(f.store.size,1);
 fail=true;await f.all('button')[0].onclick();assert.equal(f.cards()[0].dataset.state,'stale');
 assert(f.all('div').some(x=>x.className==='balance-amount'&&x.textContent==='5.00'));
 assert(!JSON.stringify(f.pool).includes('sensitive-upstream-message'));assert(queries.every(q=>q.includes('providerId=provider')&&q.includes('keyId=')));
});
test('RAM remains per-key and secrets are cleared before save; successful account stays editable',async()=>{
 let saved=null;const f=fixture(async(path,options)=>{
  if(path==='/provider-billing-credentials'){saved=JSON.parse(options.body);return{};}
  const id=new URLSearchParams(path.split('?')[1]).get('keyId');return{supported:true,providerId:'provider',keyId:id,...(saved&&saved.keyId===id?{balanceAuth:'aliyun-rpc',balance_infos:[{currency:'CNY',total_balance:'20'}]}:{billingCredentialsRequired:true,balance_infos:[]})};
 });await f.start();assert.equal(f.cards()[0].dataset.state,'pending');
 const inputs=f.cards()[0].querySelectorAll('input');inputs[0].value='synthetic-id';inputs[1].value='synthetic-secret';
 await f.cards()[0].querySelectorAll('button')[0].onclick();assert.equal(saved.keyId,'0');assert.equal(inputs[0].value,'');assert.equal(inputs[1].value,'');assert.equal(f.cards()[0].querySelectorAll('details')[0].hidden,false);assert.equal(f.cards()[1].dataset.state,'pending');assert.equal(f.store.size,0);
});
test('refresh concurrency is bounded; hidden or detached pool stops requests and aborts',async()=>{
 let running=0,max=0,calls=0;const releases=[],signals=[];
 const f=fixture((path,options)=>{calls++;running++;max=Math.max(max,running);signals.push(options.signal);return new Promise(resolve=>releases.push(()=>{running--;const id=new URLSearchParams(path.split('?')[1]).get('keyId');resolve({supported:true,providerId:'provider',keyId:id,balance_infos:[{currency:'USD',total_balance:'4'}]});}));},{count:5});
 await f.start();assert.equal(calls,2);
 while(releases.length){releases.shift()();await flush();}
 assert.equal(calls,5);assert.equal(max,2);
 f.pool.isConnected=false;f.observers[0]();assert(signals.every(s=>s.aborted));
 const hidden=fixture(async()=>{throw Error('must not query');},{active:()=>false});await hidden.start();assert.equal(hidden.cards()[0].dataset.state,undefined);
});
test('key identity mismatch never populates another card',async()=>{
 const f=fixture(async()=>({supported:true,providerId:'provider',keyId:'other',balance_infos:[{currency:'CNY',total_balance:'999'}]}));await f.start();assert(f.cards().every(c=>c.dataset.state==='stale'));assert.equal(f.all('div').filter(x=>x.className==='balance-amount').length,0);
});
test('invalid alert edits are rejected; storage failure keeps session settings with a visible warning',async()=>{
 const f=fixture(async(path)=>({supported:true,providerId:'provider',keyId:new URLSearchParams(path.split('?')[1]).get('keyId'),balance_infos:[{currency:'CNY',total_balance:'5'}]}));await f.start();
 const input=f.all('input').find(x=>x.attributes['aria-label']==='Provider / Key 0 / CNY / 报警线');
 const apply=f.all('button').find(x=>x.attributes['aria-label']==='Provider / Key 0 / CNY / 应用报警设置');
 input.value='';await apply.onclick();assert.equal(f.cards()[0].dataset.state,'low');assert.equal(f.store.size,0);
 f.store.set=()=>{throw Error('storage full');};input.value='1';await apply.onclick();assert.equal(f.cards()[0].dataset.state,'ready');
 assert(f.all('p').some(x=>x.className==='balance-pool-error'&&x.textContent.includes('无法保存报警线')));
 await f.all('button')[0].onclick();assert.equal(f.cards()[0].dataset.state,'ready');
});

test('closing the settings sheet clears unsaved RAM fields',async()=>{
 const f=fixture(async path=>({supported:true,providerId:'provider',keyId:new URLSearchParams(path.split('?')[1]).get('keyId'),billingCredentialsRequired:true,balance_infos:[]}));
 await f.start();const inputs=f.cards()[0].querySelectorAll('input');inputs[0].value='synthetic-id';inputs[1].value='synthetic-secret';
 f.sheet.open=false;f.observers[1]();assert.equal(inputs[0].value,'');assert.equal(inputs[1].value,'');
 const app=fs.readFileSync('internal/server/web/app.js','utf8');assert.match(app,/active:\(\)=>!lockScreen.locked && !!\$\('settings-sheet'\)\?\.classList.contains\('open'\)/);
});

test('plugin quota windows render as percentages without currency amounts or pricing controls',async()=>{
 const f=fixture(async path=>({supported:true,kind:'quota',supporter:'Fixture quota',providerId:'provider',keyId:new URLSearchParams(path.split('?')[1]).get('keyId'),quota_infos:[{id:'shared/primary',label:'Shared',window:'primary',remainingPercent:75,windowDurationMins:300,resetsAt:1793450000},{id:'shared/secondary',label:'Shared',window:'secondary',remainingPercent:5,windowDurationMins:10080}]}));
 await f.start();assert(f.cards().every(c=>c.dataset.state==='low'));assert(f.all('div').some(x=>x.className==='balance-amount'&&x.textContent==='75.0%'));assert.equal(f.all('input').filter(x=>x.attributes['aria-label']?.includes('报警线')).length,0);assert(f.all('span').some(x=>x.textContent==='账户共享额度 · 剩余百分比'));
});
test('pricing-only and unavailable quota are distinct and unknown never becomes zero',async()=>{
 const p=fixture(async()=>({supported:false,pricingSupported:true,supporter:'Pricing fixture'}));await p.start();assert(p.all('span').some(x=>x.textContent==='仅支持计价'));
 const q=fixture(async path=>({supported:true,kind:'quota',providerId:'provider',keyId:new URLSearchParams(path.split('?')[1]).get('keyId'),quota_infos:[]}));await q.start();assert(q.all('span').some(x=>x.textContent==='额度不可查'));assert.equal(q.all('div').filter(x=>x.className==='balance-amount').length,0);
});
test('expired quota response cannot populate a current card',async()=>{
 const q=fixture(async path=>({supported:true,kind:'quota',checkedAt:'2020-01-01T00:00:00Z',providerId:'provider',keyId:new URLSearchParams(path.split('?')[1]).get('keyId'),quota_infos:[{id:'q',remainingPercent:99}]}));await q.start();assert(q.cards().every(c=>c.dataset.state==='stale'));assert.equal(q.all('div').filter(x=>x.className==='balance-amount').length,0);
});
