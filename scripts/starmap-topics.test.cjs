'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
class Node{constructor(tag='div'){this.tag=tag;this.children=[];this.attrs={};this.handlers={};this._value='';this.textContent='';this.open=false;this.disabled=false;this.selectionStart=0;this.selectionEnd=0;}set value(v){this._value=this.tag==='textarea'?v.replace(/\r\n?/g,'\n'):v;}get value(){return this._value;}append(...n){this.children.push(...n);}replaceChildren(...n){this.children=n;this.value='';}setAttribute(k,v){this.attrs[k]=v;}addEventListener(k,f){this.handlers[k]=f;}setSelectionRange(a,b){this.selectionStart=a;this.selectionEnd=b;}}
const host=new Node('details'),events={},nodes=[];
const sandbox={window:{},document:{getElementById:id=>id==='timeline-topics'?host:null,createElement:tag=>{const n=new Node(tag);nodes.push(n);return n;}},AbortController,URLSearchParams,setTimeout,clearTimeout,addEventListener:(k,f)=>events[k]=f};
vm.runInNewContext(fs.readFileSync('internal/server/web/starmap-topics.js','utf8'),sandbox);
const byLabel=s=>nodes.find(n=>n.attrs['aria-label']===s),byText=s=>nodes.find(n=>n.textContent===s),flush=()=>new Promise(r=>setImmediate(r));
const candidate=(id,text)=>({token:id,node:'F1',root:'source',source:'fixture-source',path:'说明.md',sourceKind:'document_excerpt',quote:{heading:'文档摘录',text,partial:false}});
(async()=>{
 let queue=[],saved=0;
 const module=sandbox.window.AideKnowledgeTopics.create({request:(path,options)=>new Promise((resolve,reject)=>queue.push({path,options,resolve,reject})),onSaved:()=>saved++});
 host.open=true;module.sync({workspace:'w',code:false,before:'a',after:'b'});assert.equal(queue.length,1);
 queue[0].resolve({revision:0,topics:[],before:[candidate('c1','甲\r\n😀正文')],after:[candidate('c2','乙😀正文')],partial:false});await flush();
 byLabel('主题名称').value='<b>人工主题</b>';byLabel('主题名称').oninput();
 for(const [side,id] of [['此前','c1'],['之后','c2']]){const select=byLabel(side+'历史片段');select.value=id;select.onchange();const text=byLabel(side+'历史原文');assert.equal(text.readOnly,true);text.selectionStart=1;text.selectionEnd=side==='此前'?4:3;const parent=host.children[3].children[2].children[side==='此前'?0:1];await parent.children[4].onclick();}
 assert.ok(nodes.some(n=>n.textContent.includes('引用来源 · fixture-source · F1')));
 const form=nodes.find(n=>n.tag==='form'),save=byText('保存主题对齐');assert.equal(save.disabled,false);const pending=form.onsubmit({preventDefault(){}});assert.equal(queue.length,2);const sent=JSON.parse(queue[1].options.body);assert.equal(sent.before.start,1);assert.equal(sent.before.end,5);assert.equal(sent.expected,0);assert.equal(sent.label,'<b>人工主题</b>');
 queue[1].resolve({saved:true,id:'topic',revision:1,topics:[{id:'topic',label:sent.label,members:[]} ]});await pending;assert.equal(saved,1);
 // Changing scope while a read is pending discards its late result and old excerpts.
 const reading=byText('读取主题与历史片段').onclick();assert.equal(queue.length,3);module.sync(null);queue[2].resolve({revision:99,topics:[{id:'leak',label:'OLD'}],before:[candidate('leak','PRIVATE')],after:[]});await reading;assert.equal(byLabel('此前历史原文').value,'');assert.equal(byLabel('主题名称').value,'');assert.equal(save.disabled,true);
 module.sync({workspace:'other',code:true,before:'c',after:'d'});assert.match(queue[3].path,/code=1/);queue[3].reject(Object.assign(Error('stop'),{name:'AbortError'}));await flush();assert.match(byText('读取已取消或超时，请重试。').textContent,/超时/);events.pagehide();
 console.log('PASS manual topic UTF16 selection, explicit save, literal labels, revision, scope cancellation and stale reads');
})().catch(e=>{console.error(e);process.exitCode=1;});
