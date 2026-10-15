const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
function fn(name) {
  const start = source.search(new RegExp('^(?:async )?function '+name+'\\(', 'm'));
  assert.ok(start >= 0, name);
  return source.slice(start, source.indexOf('\n}', start) + 2);
}
function setup() {
  const nodes = new Map();
  const $ = id => {
    if (!nodes.has(id)) nodes.set(id, {value:'', textContent:'', title:'', style:{}, classList:{add(){},remove(){},toggle(){}}, children:[], replaceChildren(){this.children=[];}, removeAttribute(){},setAttribute(){}, append(...items){this.children.push(...items);}});
    return nodes.get(id);
  };
  const requests = [];
  const state = {config:{configured:true,workspaceId:'one',model:'model'},session:{id:'A',messages:[]},contextMeterSeq:{value:0},previewSeq:{value:0}};
  const sandbox = {state, $, el:(tag,cls,text)=>({tag,className:cls,textContent:text,children:[],append(...items){this.children.push(...items)}}), window:{}, AbortController, clearTimeout, setTimeout, fmtTokens:String,activeModel:()=>null, t:(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n]), contextParts:()=>[], ensureContextMeterControls(){}, renderContextCompactionState(){},hideCtxTooltip(){},updateSendEnabled(){},api:(path, options)=>new Promise((resolve,reject)=>requests.push({resolve,reject,body:JSON.parse(options.body),signal:options.signal}))};
  vm.createContext(sandbox);
  for(const name of ['sessionModelPreferences','contextMeterKey','renderParallelContextDetail','renderContextMeter','cancelContextPreview','hideContextPreview','contextMeterScope','contextMeterTransitioning','clearContextMeterDisplay','resetContextMeter','refreshContextMeter','estimateContext','refreshContextPreview','scheduleContextPreview']) vm.runInContext(fn(name),sandbox);
  return {sandbox,state,requests,$};
}
const data = inputEstimate => ({breakdown:{}, inputEstimate, contextWindow:10000});
(async()=>{
  const {sandbox:c,state:s,requests:r,$} = setup();
  c.estimateContext(); assert.equal(r.length,1);
  r[0].resolve(data(100)); await new Promise(setImmediate);
  assert.equal(s.contextMeter.inputEstimate,100);
  c.estimateContext(); assert.equal(r.length,1,'completed baseline is cached');
  s.pendingSessionId='B'; c.resetContextMeter();
  assert.equal($('context-stat').textContent,'—');
  assert.equal(s.contextPreview,null);
  c.estimateContext(); assert.equal(r.length,1,'no request for old session while loading B');
  s.session={id:'B',messages:[]};s.pendingSessionId='';c.estimateContext();
  c.estimateContext();assert.equal(r.length,2,'deduplicate pending B request');
  assert.equal(s.contextMeter,null,'never reuse A as B');
  r[1].reject(new Error('offline'));await new Promise(setImmediate);
  c.estimateContext();assert.equal(r.length,3,'failed request retries');
  r[2].resolve(data(200));await new Promise(setImmediate);assert.equal(s.contextMeter.inputEstimate,200);
  c.renderContextMeter(data(999),true);s.contextPreview=data(999);c.hideContextPreview();
  assert.equal(s.contextPreview,null);assert.equal(r.length,4,'draft is not baseline cache');
  r[3].resolve(data(200));await new Promise(setImmediate);assert.equal(s.contextMeter.inputEstimate,200);
  s.session={id:'A',messages:[]};c.estimateContext();const late=r.at(-1);
  s.session={id:'B',messages:[]};c.estimateContext();const current=r.at(-1);
  late.resolve(data(111));await new Promise(setImmediate);assert.equal(s.contextMeter,null,'late A response ignored');
  current.resolve(data(222));await new Promise(setImmediate);assert.equal(s.contextMeter.inputEstimate,222);
  s.config.workspaceId='two';c.estimateContext();assert.equal(s.contextMeter,null,'same session ID in different workspace has no cache');
  const previous=r.at(-1);s.pendingSessionId='C';previous.resolve(data(333));await new Promise(setImmediate);
  assert.equal(s.contextMeter,null,'pending selection prevents old baseline repaint');
  const fresh=setup();fresh.$('prompt').value='draft';const draft=fresh.sandbox.refreshContextPreview();
  fresh.state.session={id:'B',messages:[]};fresh.requests[0].resolve(data(888));await draft;
  assert.equal(fresh.state.contextPreview,undefined,'draft response cannot cross sessions');
  const debounce=setup();debounce.$('prompt').value='old';const old=debounce.sandbox.refreshContextPreview();
  debounce.sandbox.scheduleContextPreview();clearTimeout(debounce.state.previewTimer);
  debounce.requests[0].resolve(data(777));await old;
  assert.equal(debounce.state.contextPreview,undefined,'reschedule invalidates response before debounce runs');
  const unconfigured=setup();unconfigured.sandbox.estimateContext();unconfigured.state.config.configured=false;unconfigured.sandbox.estimateContext();
  unconfigured.requests[0].resolve(data(444));await new Promise(setImmediate);assert.equal(unconfigured.state.contextMeter,null);
  assert.match(source,/if \(!sameSession && typeof resetContextMeter === 'function'\) resetContextMeter\(\)/);
  const parallel=setup();
  parallel.state.session={id:'parallel',messages:[],sessionUsage:{prompt:500000,completion:12000,total:512000},runs:[{id:'ten',parallelism:10,parallel:{plannerContext:{inputEstimate:900,contextWindow:65536,usage:{prompt:1000}},parts:Array.from({length:10},(_,i)=>({context:{inputEstimate:2000+i,contextWindow:65536}})),mainContext:{inputEstimate:3000,contextWindow:65536,usage:{prompt:4000}}}}]};
  parallel.sandbox.renderContextMeter(data(2500),false);
  assert.equal(parallel.$('context-stat').textContent,'2500 / 10000','cumulative usage is never window occupancy');
  const details=parallel.$('context-card-detail').children.map(x=>x.textContent).join('\n');
  assert.match(details,/各路独立上下文/);assert.match(details,/子分析 10 路/);assert.match(details,/2000–2009/);assert.match(details,/最近主汇总请求：输入 4000/);assert.match(details,/500,000/);
  parallel.state.session.runs=[{parallelism:5}];parallel.sandbox.renderContextMeter(data(2500),false);assert.match(parallel.$('context-card-detail').children.map(x=>x.textContent).join(''),/未记录逐请求上下文/);
  parallel.state.session={id:'ordinary',messages:[]};parallel.sandbox.renderContextMeter(data(100),false);assert.equal(parallel.$('context-card-detail').children.length,0,'no parallel details leak across sessions');
  console.log('context meter: cache, failure retry, draft, session/workspace races and debounce passed');
})().catch(error=>{console.error(error);process.exitCode=1;});
