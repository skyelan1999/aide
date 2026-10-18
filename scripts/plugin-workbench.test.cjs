'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs'),path=require('node:path');
function fixture(response={protocol:1,url:'/plugin-workbench/'+'a'.repeat(64)}){
 const nodes=[],events={},focus={count:0,focus(){this.count++;}};
 function node(tag){const n={tag,children:[],attributes:{},append(...a){this.children.push(...a);},setAttribute(k,v){this.attributes[k]=v;},focus(){},remove(){this.removed=true;}};nodes.push(n);return n;}
 const document={activeElement:focus,createElement:node,body:node('body'),addEventListener:(k,f)=>events[k]=f};let interval,blocked=false,scope='one',enabled=true;const errors=[];
 const window={},ctx={window,document,AbortController,setInterval:f=>(interval=f,1),clearInterval:()=>{}};vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../internal/server/web/plugin-workbench.js'),'utf8'),ctx);
 return{controller:window.AidePluginWorkbench,nodes,events,focus,errors,services:{api:async()=>response,t:x=>x,toast:x=>errors.push(x),blocked:()=>blocked,scope:()=>scope,enabled:()=>enabled},tick:()=>interval(),block:()=>blocked=true,changeScope:()=>scope='two',disable:()=>enabled=false};
}
const plugin={id:'test',enabled:true,workbench:{protocol:1,title:'Test'}};
test('workbench uses sandbox without same-origin or credentials and close unloads the frame',async()=>{const f=fixture();await f.controller.open(plugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');assert.equal(frame.attributes.sandbox,'allow-scripts allow-downloads');assert.equal(frame.src,'/plugin-workbench/'+'a'.repeat(64));f.controller.close();assert.equal(frame.src,'about:blank');assert.equal(f.focus.count,1);});
test('lock, scope changes, disabled plugins and Escape close the active workbench',async()=>{for(const action of ['block','changeScope','disable']){const f=fixture();await f.controller.open(plugin,f.services);f[action]();f.tick();assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');}const f=fixture();await f.controller.open(plugin,f.services);f.events.keydown({key:'Escape',preventDefault(){}});assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');});
test('workbench refuses arbitrary routes returned by a broken API',async()=>{for(const url of ['https://evil.invalid/','/api/config','/plugin-workbench/short']){const f=fixture({protocol:1,url});await f.controller.open(plugin,f.services);assert.equal(f.errors.length,1);assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');}});
