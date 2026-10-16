const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(process.argv[2]||'internal/server/web/app.js','utf8');
function fn(name){return source.match(new RegExp('function '+name+'\\([^]*?\\n}'))?.[0]||'';}
let step=fn('renderStepThinking');
if(!step){const start=source.indexOf('        if (step.reasoning) {');step='function renderStepThinking(box,run,step,index){\n'+source.slice(start,source.indexOf('        const stages',start))+'\n}';}
class Element{
 constructor(tag,cls='',text=''){this.tagName=tag;this.className=cls;this.textContent=text;this.children=[];this.dataset={};this.listeners={};this.isConnected=true;this.classList={add(){},toggle(){}};this.scrollHeight=this.clientHeight=this.scrollTop=0;}
 append(...nodes){for(const node of nodes){node.parentNode=this;this.children.push(node);}}
 prepend(node){node.parentNode=this;this.children.unshift(node);}
 replaceChildren(...nodes){this.children=[];this.append(...nodes);}
 querySelectorAll(selector){const match=node=>selector.startsWith('.')?node.className.split(' ').includes(selector.slice(1)):node.tagName===selector;return this.children.flatMap(node=>[...(match(node)?[node]:[]),...node.querySelectorAll(selector)]);}
 querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
 setAttribute(){} removeAttribute(){} addEventListener(name,fn){this.listeners[name]=fn;}
 remove(){if(this.parentNode)this.parentNode.children=this.parentNode.children.filter(n=>n!==this);}
}
const run={id:'r',mode:'chat',status:'running',steps:[{name:'chat',reasoning:'saved thinking'}],toolUses:[{tool:'read_file',result:'one'},{tool:'read_file',result:'two'}]};
const phase={startedAt:0,lastActivity:0,phase:'reasoning',tools:[{callId:'one',tool:'read_file',status:'done'},{callId:'two',tool:'read_file',status:'done'}]};
const ctx={state:{session:{runs:[run]},runPhase:{r:phase},liveReasoning:{r:'saved thinking'},thinkingOpen:new Set()},el:(tag,cls,text)=>new Element(tag,cls,text),t:s=>s,phaseLabel:()=>'',formatElapsed:()=>'',runWaitingForUser:()=>false,thinkAtBottom:()=>true,document:{createTextNode:text=>new Element('text','',text)},Date};
vm.createContext(ctx);vm.runInContext([fn('renderToolRow'),fn('runReasoningText'),step,fn('renderRunStatusInto'),fn('updateThinkingSummary'),fn('bindThinkingDisclosure')].join('\n'),ctx);
const box=new Element('article');ctx.renderRunStatusInto(box,'r');ctx.renderStepThinking(box,run,run.steps[0],0);
assert.equal(box.querySelectorAll('.rsp-thinking').length,1,'live and persisted reasoning must share one card');
assert.equal(box.querySelectorAll('.rsp-tool').length,0,'finished calls belong to persistent tool history, not duplicate live rows');
phase.tools.push({callId:'third',tool:'read_file',status:'running',startedAt:0});ctx.renderRunStatusInto(box,'r');ctx.renderRunStatusInto(box,'r');
assert.equal(box.querySelectorAll('.run-status-panel').length,1);assert.equal(box.querySelectorAll('.rsp-tool').length,1,'only active tool is in live panel');
ctx.state.liveReasoning.r='';ctx.renderRunStatusInto(box,'r');assert.equal(box.querySelectorAll('.rsp-thinking').length,1,'poll-only recovery retains thinking');
ctx.state.thinkingOpen.add('r');const det=box.querySelector('.rsp-thinking');det.open=true;det.listeners.toggle();assert.equal(det.querySelector('.rsp-think-body').textContent,'saved thinking','opening recovered thinking loads persisted body');
assert.equal(ctx.runReasoningText('r'),'saved thinking');ctx.state.liveReasoning.r='new longer thinking text';assert.equal(ctx.runReasoningText('r'),'new longer thinking text');
const done=new Element('article');run.status='completed';ctx.renderStepThinking(done,run,run.steps[0],0);assert.equal(done.querySelectorAll('.rsp-thinking').length,1,'terminal evidence remains visible');
run.status='running';delete ctx.state.runPhase.r;const fallback=new Element('article');ctx.renderStepThinking(fallback,run,run.steps[0],0);assert.equal(fallback.querySelectorAll('.rsp-thinking').length,1,'without live panel use persisted thinking');
const summary=det.querySelector('summary');
ctx.updateThinkingSummary(det,'first line\nlatest streamed line\n\n',true);
assert.equal(summary.querySelector('.rsp-think-preview').textContent,'latest streamed line','collapsed preview follows latest nonempty line');
const label=summary.querySelector('.rsp-think-label'),excerpt=summary.querySelector('.rsp-think-preview');
ctx.updateThinkingSummary(det,'first line\n'+ 'x'.repeat(600)+'LATEST',true);
assert.equal(summary.querySelector('.rsp-think-preview'),excerpt,'streaming preserves pointer target');
assert.equal(summary.querySelector('.rsp-think-label'),label);
assert.ok(excerpt.textContent.endsWith('LATEST'));assert.equal(excerpt.textContent.length,110);
ctx.state.thinkingOpen.clear();det.open=false;
let prevented=false;summary.listeners.click({preventDefault(){prevented=true}});
assert.ok(prevented);assert.equal(det.open,true);assert.ok(ctx.state.thinkingOpen.has('r'),'intent saved before asynchronous toggle/poll');
det.isConnected=false;det.open=false;det.listeners.toggle();
assert.ok(ctx.state.thinkingOpen.has('r'),'detached predecessor cannot erase new disclosure state');
det.isConnected=true;det.open=true;summary.listeners.click({preventDefault(){}});
assert.equal(det.open,false);assert.ok(!ctx.state.thinkingOpen.has('r'));assert.equal(det.querySelector('.rsp-think-body').children.length,0);
run.steps.push({reasoning:'later step'});ctx.state.thinkingOpen.add('r');const history=new Element('article');
ctx.renderStepThinking(history,run,run.steps[0],0);assert.ok(!history.querySelector('details').open,'latest-step state does not expand older steps');
console.log('Run activity cards PASS: one thinking owner, pending tools only, poll recovery, repeat render, terminal and no-stream evidence');
