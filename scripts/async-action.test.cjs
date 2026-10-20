'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const scope={window:{}};vm.runInNewContext(fs.readFileSync('internal/server/web/async-action.js','utf8'),scope);
const {wrap}=scope.window.AideAsyncAction;
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b;});return{promise,resolve,reject};};
class Element{
 constructor(tag){this.tag=tag;this.attrs=new Map();this.disabled=false;}
 matches(selector){return selector.split(',').some(s=>s.trim().split('[')[0]===this.tag);}
 getAttribute(k){return this.attrs.get(k)??null;}
 setAttribute(k,v){this.attrs.set(k,v);}
 removeAttribute(k){this.attrs.delete(k);}
 querySelector(){return this.submit;}
}
const event=(button,type='click',submitter)=>({currentTarget:button,type,submitter,preventDefault(){this.prevented=true;}});
(async()=>{
 const a=new Element('button'),b=new Element('button'),first=deferred(),second=deferred();let calls=0;
 const handler=wrap(e=>{calls++;assert.equal(e.currentTarget.getAttribute('data-action-pending'),null,'handler guards run before shared feedback');e.preventDefault();return calls===1?first.promise:second.promise;});
 const click=event(a),pa=handler(click);assert.equal(click.prevented,true);assert.equal(a.getAttribute('aria-busy'),'true');assert.equal(b.getAttribute('aria-busy'),null);assert.equal(a.disabled,false,'do not override handler-owned disabled state');
 click.currentTarget=null;await handler(event(a));await wrap(()=>{throw Error('duplicate');})(event(a));assert.equal(calls,1,'same element guarded even across wrappers');
 const pb=handler(event(b));assert.equal(calls,2);first.resolve();await pa;assert.equal(a.getAttribute('aria-busy'),null);assert.equal(b.getAttribute('aria-busy'),'true');second.resolve();await pb;
 let errors=0;const failure=deferred(),failed=wrap(()=>failure.promise,()=>errors++);const pf=failed(event(a));failure.reject(Error('network'));await pf;assert.equal(errors,1);assert.equal(a.getAttribute('data-action-pending'),null);
 await wrap(()=>{throw Error('synchronous');},()=>errors++)(event(a));assert.equal(errors,2);
 a.setAttribute('aria-busy','false');await wrap(async()=>{a.disabled=true;})(event(a));assert.equal(a.getAttribute('aria-busy'),'false');assert.equal(a.disabled,true,'business state is retained');a.disabled=false;
 a.removeAttribute('aria-busy');await wrap(async()=>{a.setAttribute('aria-busy','true');})(event(a));assert.equal(a.getAttribute('aria-busy'),'true','existing handler feedback is retained');a.removeAttribute('aria-busy');
 const form=new Element('form');form.submit=a;let submits=0;const saved=deferred();const save=wrap(e=>{e.preventDefault();submits++;return saved.promise;});const ps=save(event(form,'submit'));assert.equal(a.getAttribute('aria-busy'),'true');await save(event(form,'submit',b));assert.equal(submits,1,'Enter/alternate submitter cannot duplicate pending form');saved.resolve();await ps;
 let sync=false;const direct=wrap(()=>{sync=true;});direct(event(a));assert.equal(sync,true);assert.equal(a.getAttribute('data-action-pending'),null,'sync navigation needs no artificial delay');
 const background=deferred();let reads=0;const load=wrap(()=>{reads++;return background.promise;});const r1=load(),r2=load();assert.equal(reads,2,'background refresh has no implicit global lock');background.resolve();await Promise.all([r1,r2]);
 const navigation=deferred();let opens=0;const open=wrap(()=>{opens++;return navigation.promise;},undefined,{feedback:false});const o1=open(event(a)),o2=open(event(a));assert.equal(opens,2,'navigation with its own generations can close/reopen while loading');assert.equal(a.getAttribute('data-action-pending'),null);navigation.resolve();await Promise.all([o1,o2]);
 console.log('PASS immediate local feedback, synchronous activation, per-element/form duplicate guard, independent buttons, failure recovery and handler-owned state');
})().catch(e=>{console.error(e);process.exitCode=1;});
