'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');

function fakeNode(tag){
 return {tag,children:[],hidden:false,open:false,className:'',textContent:'',title:'',type:'',disabled:false,innerHTML:'',value:'',attrs:{},onclick:null,
  append(...items){this.children.push(...items);},replaceChildren(...items){this.children=items;},setAttribute(k,v){this.attrs[k]=v;},
  addEventListener(){},classList:{toggle(){},add(){},remove(){}},showModal(){this.open=true;},close(){this.open=false;}};
}
const tick=async()=>{for(let i=0;i<40;i++)await Promise.resolve();};
(async()=>{
 let calls=0,status='unavailable';
 const document={createElement:fakeNode,getElementById:()=>fakeNode('div'),visibilityState:'visible',addEventListener(){}};
 const window={};
 const context={window,document,MutationObserver:undefined,setTimeout,clearTimeout,Promise,Error,String,Math,Number,Date,Array,Object,JSON};
 vm.createContext(context);
 vm.runInContext(fs.readFileSync('internal/server/web/assistant-memory.js','utf8'),context);

 const builtin=context.window.AideAssistantMemory.create({translate:x=>x,valid:()=>true,session:()=>'assistant',engine:()=>'builtin',api:async()=>{calls++;return {};}});
 await builtin.sync();await tick();
 assert.equal(calls,0,'builtin engine skips the memory endpoint entirely');
 const memory=context.window.AideAssistantMemory.create({
  translate:x=>x,valid:()=>true,session:()=>'assistant',engine:()=>'mixsoul',
  api:async()=>{calls++;if(status==='unavailable')throw new Error('memory engine unavailable');return {storage:{records:0,limit:0,queueDepth:0},total:0,pageSize:20,records:[]};}
 });
 await memory.sync();await tick();
 assert.equal(calls,1,'first status probe runs');
 for(let i=0;i<20;i++)await memory.sync();
 await tick();
 assert.equal(calls,1,'builtin engine must not be retried on every assistant repaint');
 assert.equal(memory.element.hidden,true,'memory entry stays hidden when unavailable');

 status='available';memory.clear();await memory.sync();await tick();
 assert.equal(calls,2,'a new session clears the unavailable cache');
 assert.equal(memory.element.hidden,false,'memory entry appears when the engine is available');
 console.log('PASS assistant memory: unavailable engine is probed once per session');
})().catch(e=>{console.error(e);process.exitCode=1});
