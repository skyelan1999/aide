'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const src=fs.readFileSync('internal/server/web/app.js','utf8');
function fn(name){const a=src.indexOf('function '+name+'(');return src.slice(a,src.indexOf('\n}',a)+2)}
class Node {
 constructor(tag='div',cls='',text=''){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.dataset={};this.hidden=false;this.disabled=false;}
 append(...nodes){this.children.push(...nodes)} replaceChildren(...nodes){this.children=nodes} get childElementCount(){return this.children.length}
 setAttribute(){} remove(){this.removed=true} querySelectorAll(selector){return this.children.flatMap(n=>[...(['button','input','select'].includes(n.tag)?[n]:[]),...n.querySelectorAll(selector)])}
 querySelector(){return this.querySelectorAll('')[0]} scrollIntoView(){} focus(){}
}
const nodes=new Map(),$=id=>{if(!nodes.has(id))nodes.set(id,new Node());return nodes.get(id)};
const run={id:'r',prompt:'生成汽车模型',status:'awaiting_clarification',pendingQuestion:{type:'confirm',approvalKind:'shell',command:'printf safe\n'.repeat(200),questionId:'q1',approvalRound:'round1'}};
const state={session:{id:'s',runs:[run]},runPhase:{}};let calls=[];
const ctx={state,$,el:(...a)=>new Node(...a),t:(s,...a)=>s.replace(/\{(\d+)\}/g,(_,i)=>a[i]),document:{querySelectorAll:()=>[]},composerControlRun:()=>state.session?.runs[0],clarificationSubmissions:new Set(),toast(){},refreshSessionSoon(){},schedulePoll(){},api:async(path,args)=>calls.push({path,body:JSON.parse(args.body)})};vm.createContext(ctx);vm.runInContext(['approvalValidityPicker','renderClarification','syncTaskActionHub'].map(fn).join('\n'),ctx);
const all=(n,tag)=>n.children.flatMap(c=>[...(c.tag===tag?[c]:[]),...all(c,tag)]);
(async()=>{
 const history=new Node();ctx.renderClarification(run,history);assert.equal(all(history,'button').length,1);assert.equal(all(history,'pre').length,0,'history is a locator, not duplicate actions');
 ctx.syncTaskActionHub(run);const slot=$('run-action-content'),card=slot.children[0],command=all(card,'details')[0];assert.equal(all(card,'pre')[0].textContent,run.pendingQuestion.command);assert(!command.open,'full command is available on demand');
 command.open=true;ctx.syncTaskActionHub(run);assert.equal(slot.children[0],card,'polling retains command disclosure and input state');assert(command.open);
 const allow=all(card,'button').find(b=>b.textContent==='仅本次允许');await allow.onclick();assert.equal(calls.length,1);assert.equal(calls[0].body.questionId,'q1');assert.equal(calls[0].body.round,'round1');assert.equal(calls[0].body.remember,false);await allow.onclick();assert.equal(calls.length,1,'double click cannot duplicate a decision');
 run.pendingQuestion={...run.pendingQuestion,questionId:'q2'};ctx.syncTaskActionHub(run);const stale=all(slot,'button').find(b=>b.textContent==='仅本次允许');run.pendingQuestion={...run.pendingQuestion,questionId:'q3'};await stale.onclick();assert.equal(calls.length,1,'in-place question updates invalidate old actions');
 ctx.syncTaskActionHub(run);const switched=all(slot,'button').find(b=>b.textContent==='仅本次允许');state.session={id:'other',runs:[]};await switched.onclick();assert.equal(calls.length,1,'cross-session stale buttons never post');ctx.syncTaskActionHub(null);assert(slot.hidden);assert.equal(slot.children.length,0);
 state.session={id:'s',runs:[run]};run.status='running';run.parallelism=10;state.runPhase.r={tools:[{tool:'read_file',status:'running'}]};ctx.syncTaskActionHub(run);assert.match($('run-control-activity').textContent,/read_file.*10/);assert(slot.hidden);
 console.log('Task action hub PASS: unique decision UI, exact command, stable disclosure, scoped approval, dedupe, session switch and live activity');
})().catch(e=>{console.error(e);process.exitCode=1});
