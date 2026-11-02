'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element {
 constructor(tag,cls='',text=''){this.tagName=tag;this.children=[];this.textContent=text;this.classes=new Set(cls.split(' '));this.classList={toggle:(k,on)=>on?this.classes.add(k):this.classes.delete(k)};this.value='';this.checked=false;}
 get lastElementChild(){return this.children.at(-1);}append(...nodes){this.children.push(...nodes);}setAttribute(k,v){this[k]=v;}
}
const app=fs.readFileSync('internal/server/web/app.js','utf8');
const code=app.slice(app.indexOf('function renderXiaomiModelControl()'),app.indexOf("controlRenderers['xiaomi-model']"));
async function fixture(cfg,models=[{id:'a',name:'Model A',providerId:'pa'},{id:'b',name:'Model B',providerId:'pb'}]){
 const writes=[];const state={config:{models,activeModel:'a'}};
 const c={setTimeout,clearTimeout,structuredClone,state,t:(s,...args)=>s.replace(/\{(\d+)\}/g,(_,i)=>args[i]),modelProviderName:id=>id||'',el:(...args)=>new Element(...args),refreshStrategyUI(){},toast(){},action:f=>f,api:async(_path,options)=>{if(!options)return cfg;const body=JSON.parse(options.body);writes.push(body);return {...body,hasKey:false};}};
 vm.createContext(c);vm.runInContext(app.slice(app.indexOf('let settingsWriteQueue ='),app.indexOf('// 设置面板：语音助理名称输入'))+code,c);const wrap=c.renderXiaomiModelControl();for(let i=0;i<5;i++)await Promise.resolve();
 return {c,wrap,state,writes,select:wrap.children[1],fields:wrap.children[2],status:wrap.children[3],save:wrap.children[4]};
}
(async()=>{
 const f=await fixture({source:'inherit'});assert.equal(f.select.value,'inherit');assert.equal(f.select.disabled,false);assert.deepEqual(f.select.children.map(o=>o.value),['inherit','model:a','model:b']);
 f.select.value='model:b';f.select.onchange();await vm.runInContext('flushSettingsAutosaves()',f.c);assert.equal(f.writes[0].source,'configured');assert.equal(f.writes[0].model,'b');assert.equal(f.writes[0].apiKey,undefined);assert.equal(f.state.config.activeModel,'a');
 const loaded=await fixture({source:'configured',model:'b'});assert.equal(loaded.select.value,'model:b');
 const missing=await fixture({source:'configured',model:'gone'});assert.equal(missing.select.value,'model:gone');assert.equal(missing.select.children.at(-1).disabled,true);
 const custom=await fixture({source:'custom',model:'own',hasKey:true});assert.equal(custom.select.value,'custom');assert.equal(custom.select.children.at(-1).textContent.includes('own'),true);assert.equal(custom.writes.length,0,'opening legacy custom choice must not overwrite it');
 const empty=await fixture({source:'inherit'},[]);assert.deepEqual(empty.select.children.map(o=>o.value),['inherit']);
 console.log('PASS assistant picker: configured catalog, provider labels, saved selection, missing model, custom compatibility and no workbench mutation');
})().catch(e=>{console.error(e);process.exitCode=1;});
