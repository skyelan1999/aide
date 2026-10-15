'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const moduleSource=fs.readFileSync('internal/server/web/starmap-assist.js','utf8'),source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const c=vm.createContext({window:{},AbortController,setTimeout,clearTimeout});vm.runInContext(moduleSource,c);const assist=c.window.AideStarmapAssist;
let busy=false,scope='';const owner=assist.create({busy:v=>busy=v,scope:v=>scope=v});
const first=owner.start('first');owner.updated();assert.equal(first.controller.signal.aborted,false);assert.match(scope,/索引已有更新/);
owner.cancel();assert.equal(busy,false);assert.equal(owner.current(first),false);
const second=owner.start('second');owner.finish(first);assert.equal(busy,true);assert.equal(scope,'second');assert.equal(owner.current(second),true);
const third=owner.start('third');assert.equal(second.controller.signal.aborted,true);owner.finish(second);assert.equal(busy,true);third.controller.abort();assert.equal(busy,false);
const file={id:'F1',name:'file.md',kind:'file',origin:'o'},dir={id:'D1',kind:'directory'};
assert.throws(()=>assist.selection({results:[],question:'why'}),/先选择/);
assert.throws(()=>assist.selection({find:true,results:[]}),/输入/);
assert.equal(assist.selection({selected:file,results:[]}).ids[0],'F1');
assert.equal(assist.selection({results:[dir,...Array(12).fill(file)],question:'how'}).ids.length,8);
assert.equal(assist.selection({find:true,selected:file,question:'why'}).ids.length,0);
assert.equal(assist.selection({docs:true,results:[file],question:'why'}).ids.length,0);
// A minimal DOM verifies that hostile answer markup/URLs stay inert, and only
// references actually returned by the API become actionable buttons.
class Element{
 constructor(tag,owner){this.tag=tag;this.ownerDocument=owner;this.children=[];this.textContent='';}
 append(...v){this.children.push(...v);}replaceChildren(){this.children=[];}
}
const doc={createElement(tag){return new Element(tag,doc);},createTextNode(text){return {tag:'#text',textContent:text};}};
const target=new Element('div',doc);let clicked=null;
assist.render(target,'## Evidence\n**Known** [F1] [fake] <img src=x onerror=alert(1)>\n\n- `code`\n```js\n<script>alert(1)</script>\n```',[file],r=>clicked=r.id);
function all(el){return [el,...(el.children||[]).flatMap(all)];}
assert.equal(all(target).filter(x=>x.tag==='button').length,1);all(target).find(x=>x.tag==='button').onclick();assert.equal(clicked,'F1');
assert.equal(all(target).filter(x=>['img','script','a'].includes(x.tag)).length,0);assert.ok(all(target).some(x=>x.tag==='strong'));assert.ok(all(target).some(x=>x.tag==='pre'));
// Exercise the actual async integration, with a transport that ignores abort.
const els=new Map(),el=id=>{if(!els.has(id))els.set(id,{value:'',disabled:false,hidden:false,textContent:'',children:[],focus(){},setAttribute(){},addEventListener(){},replaceChildren(){this.children=[]},append(x){this.children.push(x)}});return els.get(id);};
const pending=[];let rendered=[];
Object.assign(c,{$:el,locked:false,graph:{workspace:'W'},codeView:false,region:'workspace',selected:file,results:[file],aiAbort:null,retrievalMode:()=>c.mode||'nodes',api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject})),renderDocumentResults(){},nodeByID:new Map([['F1',file]]),select(){},say(){},document:doc,appendSourceLabel(){}});
c.window.AideStarmapAssist={...assist,render:(_el,text)=>rendered.push(text)};
vm.runInContext(source.slice(source.indexOf('const aiExplorer='),source.indexOf('let insertionSessions=[]')),c);
(async()=>{
 const old=vm.runInContext('askAI()',c);assert.equal(el('ask').disabled,true);
 vm.runInContext('aiExplorer.updated()',c);assert.equal(c.aiAbort.signal.aborted,false);assert.match(el('ai-scope').textContent,/索引已有更新/);
 el('ai-stop').onclick();assert.equal(el('ask').disabled,false);const current=vm.runInContext('askAI()',c);
 pending[0].resolve({answer:'old',references:[]});await old;assert.deepEqual(rendered,[]);assert.equal(el('ask').disabled,true);
 pending[1].resolve({answer:'current',references:[],usage:{total:23},model:'test'});await current;assert.deepEqual(rendered,['current']);assert.equal(el('ask').disabled,false);assert.match(el('usage').textContent,/23 tokens/);
 const failed=vm.runInContext('askAI()',c);el('ai-stop').onclick();const newer=vm.runInContext('askAI()',c);pending[2].reject(Error('late error'));await failed;assert.equal(el('answer').textContent,'正在读取资料并生成回答…');assert.equal(el('ask').disabled,true);
 c.locked=true;vm.runInContext('aiExplorer.cancel()',c);el('answer').textContent='';pending[3].resolve({answer:'secret'});await newer;assert.equal(el('answer').textContent,'');assert.equal(rendered.length,1);
 c.locked=false;const switched=vm.runInContext('askAI()',c);c.graph.workspace='other';pending[4].resolve({answer:'wrong workspace'});await switched;assert.equal(rendered.length,1);
 let deadline;c.setTimeout=fn=>{deadline=fn;return 1};c.clearTimeout=()=>{};
 const timed=vm.runInContext('askAI()',c);deadline();assert.equal(el('ask').disabled,false);assert.match(el('answer').textContent,/超时/);pending[5].resolve({answer:'late after timeout'});await timed;assert.equal(rendered.length,1);assert.match(el('answer').textContent,/超时/);
 console.log('PASS: progressive snapshots, immediate stop, stale success/error/finally ownership, lock/workspace isolation, explicit scope and inert answer citations');
})().catch(e=>{console.error(e);process.exitCode=1;});
