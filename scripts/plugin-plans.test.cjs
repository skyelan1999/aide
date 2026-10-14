'use strict';
const assert=require('node:assert/strict'), fs=require('node:fs'), vm=require('node:vm');
class Element {
 constructor(tag,cls='',text=''){this.tag=tag;this.className=cls;this.textContent=String(text);this.children=[];this.dataset={};this.listeners={};this.open=false;}
 append(...nodes){for(const n of nodes){n.parent=this;this.children.push(n);}}
 setAttribute(key,value){this[key]=value;}
 showModal(){this.open=true;}
 close(){this.open=false;}
 remove(){if(this.parent)this.parent.children=this.parent.children.filter(n=>n!==this);}
 addEventListener(type,handler){this.listeners[type]=handler;}
 querySelectorAll(selector){return walk(this).filter(n=>selector==='[data-plugin-plan-focus]'?n.dataset.pluginPlanFocus:selector==='details'?n.tag==='details':n.className.split(' ').includes(selector.slice(1)));}
 focus(options){this.focused=options;}
}
function walk(root){return [root,...root.children.flatMap(walk)];}
const el=(tag,cls,text)=>new Element(tag,cls,text);
const t=(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>String(args[n]));
const c={window:{},state:{historyLimit:30},Math};vm.createContext(c);
vm.runInContext(fs.readFileSync('internal/server/web/plugin-plans.js','utf8'),c);
const api=c.window.AidePluginPlans;
assert.equal(api.render({},{el,t}),null);
const run={id:'run-A',status:'running',steps:[],pluginPlans:[{id:'plan',state:'running',requested:5,limit:1,reason:'Hook serialization',nodes:[
 {id:'a',title:'<img src=x onerror=alert(1)>',tool:'read_part',state:'succeeded',result:'A'.repeat(5000),arguments:{secret:'never display'},proposals:[{content:'never display'}]},
 {id:'b',tool:'write_part',state:'proposed',dependsOn:['a'],mergeState:'queued'},
 {id:'c',tool:'external_action',state:'uncertain',dependsOn:['b'],error:'<script>evil()</script>'},
 {id:'d',tool:'pending',state:'dispatched',dependsOn:[]},
]}]};
let openedEvidence=0;
const panel=api.render(run,{el,t,onEvidence(){openedEvidence++;}});
const root=panel.children[0];assert.equal(root.open,true,'new active/uncertain plan opens');
const all=walk(panel), joined=all.map(n=>n.textContent).join('\n');
assert.match(joined,/请求 5x · 执行上限 1x/);assert.match(joined,/待核实 1/);assert.match(joined,/已结束 3\/4/);
assert.match(joined,/提案尚未应用/);assert.match(joined,/提案待审批/);assert.match(joined,/派发包含资源等待/);
assert.match(joined,/<img src=x onerror=alert\(1\)>/);assert.match(joined,/<script>evil\(\)<\/script>/);
assert.ok(all.every(n=>!Object.hasOwn(n,'innerHTML')),'untrusted graph content never uses HTML');assert.doesNotMatch(joined,/never display/);
assert.ok(all.find(n=>n.textContent==='已派发').title.includes('未证明处理器已进入'));
const result=all.find(n=>n.className==='plugin-node-result-detail');const pre=result.children[1];assert.equal(pre.textContent,'','closed result is not materialized');
result.open=true;result.listeners.toggle();assert.equal(pre.textContent.length,4096);result.open=false;result.listeners.toggle();assert.equal(pre.textContent,'');
const evidence=all.find(n=>n.className.includes('plugin-plan-evidence'));evidence.onclick();assert.equal(openedEvidence,1);
const collapsed=api.render(run,{el,t,seen:new Set([root.dataset.key])});assert.equal(collapsed.children[0].open,false,'user collapse survives progress');
const restored=api.render(run,{el,t,seen:new Set([root.dataset.key]),open:new Set([root.dataset.key,result.dataset.key])});
assert.equal(restored.children[0].open,true);assert.equal(walk(restored).find(n=>n.className==='plugin-node-result').textContent.length,4096);
const focusKey=walk(restored).find(n=>n.className==='plugin-node-result-detail').children[0].dataset.pluginPlanFocus;
assert.equal(api.restoreFocus(restored,focusKey),true);assert.equal(walk(restored).find(n=>n.dataset.pluginPlanFocus===focusKey).focused.preventScroll,true);
assert.equal(api.restoreFocus(restored,'other-run'),false,'stale owner focus cannot target another run');
const odd=api.render({id:'odd',pluginPlans:[{id:'p',state:'__proto__',requested:99,limit:'Infinity',nodes:[{id:'n',tool:'x',state:'constructor',mergeState:'__proto__'}]}]},{el,t});
assert.match(walk(odd).map(n=>n.textContent).join('\n'),/未知状态/);assert.match(walk(odd).map(n=>n.textContent).join('\n'),/请求 —x/);
const collision=structuredClone(run);collision.pluginPlans.push({id:'plan:node:a:result',nodes:[],state:'completed'});
const keys=walk(api.render(collision,{el,t})).filter(n=>n.dataset.key).map(n=>n.dataset.key);assert.equal(new Set(keys).size,keys.length,'structured keys avoid delimiter collisions');
vm.runInContext(fs.readFileSync('internal/server/web/locales/en.js','utf8'),c);
const english=(s,...args)=>t(c.window.aideEnglish[s]||s,...args);
assert.match(walk(api.render(run,{el,t:english})).map(n=>n.textContent).join('\n'),/Proposal not applied/);

const app=fs.readFileSync('internal/server/web/app.js','utf8');
for(const name of ['sessionPluginPlanView','sessionTextRevision','sessionRevision','sessionValueFingerprint','sessionRunRenderSignature']){
 const source=app.match(new RegExp('function '+name+'\\([^]*?\\n\\}'))?.[0];assert.ok(source,name);vm.runInContext(source,c);
}
const session={id:'session',updated:'same',messages:[],runs:[structuredClone(run)]};
const revision=()=>c.sessionRevision(session), signature=()=>c.sessionRunRenderSignature(session.runs[0]);
let before=revision(), card=signature();session.runs[0].pluginPlans[0].nodes[3].state='waiting';
assert.notEqual(revision(),before,'node-only changes invalidate session cache');assert.notEqual(signature(),card,'node-only changes invalidate card cache');
before=revision();card=signature();const node=session.runs[0].pluginPlans[0].nodes[0];node.arguments.secret='changed';node.proposals[0].content='changed';
assert.equal(revision(),before,'unrendered private arguments/proposals do not churn cache');assert.equal(signature(),card);
node.result='A'.repeat(4999)+'B';assert.equal(signature(),card,'hidden result suffix with same length is not rendered');
node.result='B'+'A'.repeat(4999);assert.notEqual(signature(),card,'same-length visible excerpt update repaints');
const old=structuredClone(run);old.id='old';session.runs.unshift(old);before=revision();old.pluginPlans[0].nodes[0].state='uncertain';assert.notEqual(revision(),before,'non-latest run graph changes repaint');

let refreshes=0;const es={addEventListener(type,fn){this[type]=fn;}};
const stream={es,run:{id:'stream-run'},state:{stream:es,runPhase:{}},refreshSessionSoon(){refreshes++;}};vm.createContext(stream);
vm.runInContext(app.slice(app.indexOf("  es.addEventListener('intent'"),app.indexOf("  es.addEventListener('reasoning'")),stream);
es.intent({data:JSON.stringify({callId:'plan/node',tool:'plugin'})});assert.equal(refreshes,1,'intent refreshes graph even without transient run-phase UI');stream.state.stream={};es.intent({data:'{}'});assert.equal(refreshes,1,'stale SSE cannot refresh another session');

let openCount=0;const outcome={state:{session:{id:'owner',runs:[{id:'run'}]}},document:{querySelectorAll(){return[];}},openTrajectory(){openCount++;}};vm.createContext(outcome);
vm.runInContext('let trajView="history",trajOutcomeRun=null;'+app.match(/function openRunOutcome\([^]*?\n\}/)[0]+';this.selection=()=>trajOutcomeRun;',outcome);
assert.equal(outcome.openRunOutcome('run','stale'),false);assert.equal(outcome.openRunOutcome('run','owner'),true);assert.equal(outcome.selection().runId,'run');assert.equal(openCount,1);
outcome.state.session={id:'other',runs:[{id:'run'}]};assert.equal(outcome.openRunOutcome('run','owner'),false,'evidence navigation is session-bound');
console.log('plugin plans: truthful states, safe/lazy excerpts, collapse/focus, cache privacy, live intents, i18n and scoped evidence navigation PASS');

(async()=>{
 const stopped=structuredClone(run);stopped.status='interrupted';stopped.pluginPlans[0].state='interrupted';stopped.pluginPlans[0].digest='a'.repeat(64);
 const n=stopped.pluginPlans[0].nodes[2];n.effectChecks=[{conclusion:'inconclusive',evidence:'<img src=x> operator evidence',recordedAt:'2026-10-15T00:00:00Z',source:'operator_observation',digest:'check1'}];
 let target;
 const checked=api.render(stopped,{el,t,onCheck:(p,node)=>{target=[p,node];}});
 const checkButton=walk(checked).find(n=>n.textContent==='记录人工核查');assert.ok(checkButton);checkButton.onclick();assert.equal(target[1].id,'c');
 assert.match(walk(checked).map(n=>n.textContent).join('\n'),/人工核查记录 1/);
 assert.ok(walk(checked).every(n=>!Object.hasOwn(n,'innerHTML')));
 const live=structuredClone(stopped);live.status='running';assert.ok(!walk(api.render(live,{el,t,onCheck(){}})).some(n=>n.textContent==='记录人工核查'));
 const checkSession={id:'s',runs:[stopped],messages:[]};const oldSig=c.sessionRunRenderSignature(stopped);n.effectChecks[0].digest='check2';assert.notEqual(c.sessionRunRenderSignature(stopped),oldSig,'operator-only updates invalidate run cache');
 let active=true,interval,cleared=0,submissions=0,finish,saved=0;
 c.document={body:el('body')};c.setInterval=fn=>(interval=fn,11);c.clearInterval=()=>{cleared++;};
 const dialog=api.openCheck({el,t,plan:stopped.pluginPlans[0],node:n,active:()=>active,submit:async payload=>{submissions++;assert.equal(payload.expected,1);assert.equal(payload.planDigest,'a'.repeat(64));assert.equal(payload.conclusion,'inconclusive');await new Promise(r=>{finish=r;});},saved:()=>{saved++;}});
 const form=walk(dialog),evidence=form.find(n=>n.tag==='textarea'),save=form.find(n=>n.textContent==='保存核查记录');
 evidence.value='';await save.onclick();assert.equal(submissions,0,'empty check cannot dispatch');
 evidence.value='Operator readback evidence';const sending=save.onclick();await save.onclick();assert.equal(submissions,1,'double click cannot append twice');finish();await sending;assert.equal(saved,1);assert.equal(evidence.value,'');assert.ok(cleared>0);
 const failed=api.openCheck({el,t,plan:stopped.pluginPlans[0],node:n,active:()=>active,submit:async()=>{throw Error('409 record changed');}});
 const failedForm=walk(failed),input=failedForm.find(n=>n.tag==='textarea');input.value='Preserve evidence on request failure';await failedForm.find(n=>n.textContent==='保存核查记录').onclick();assert.equal(input.value,'Preserve evidence on request failure');assert.ok(walk(failed).some(n=>n.textContent==='409 record changed'));
 active=false;interval();assert.equal(input.value,'');assert.equal(c.document.body.children.length,0,'session/workspace boundary closes and clears dialog');
 console.log('plugin effect checks: operator history, cache refresh, scoped dialog, double-submit, failure retention and owner invalidation PASS');
})().catch(e=>{console.error(e);process.exitCode=1;});
