'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
class Element{constructor(tag,cls='',value=''){this.tag=tag;this.className=cls;this.textContent=value;this.childNodes=[];this.classList={add:()=>{}};}append(...items){this.childNodes.push(...items);}prepend(...items){this.childNodes.unshift(...items);}replaceChildren(...items){this.childNodes=items;}setAttribute(){} }
const walk=n=>[n,...n.childNodes.flatMap(walk)];
(async()=>{
 const calls=[],wrap=new Element('div');
 const state={token:'fixture',config:{workspaceId:'fixture-workspace',agentManagement:{aidePlugins:true,assistantPlugins:false,aideReferences:false,assistantReferences:true}}};
 const context=vm.createContext({state,el:(...args)=>new Element(...args),t:x=>x,action:x=>x,toast:()=>{},api:async(path,opts)=>{calls.push([path,opts]);},refreshConfig:async()=>{},controlRenderers:{}});
 const app=fs.readFileSync('internal/server/web/app.js','utf8');vm.runInContext(app.slice(app.indexOf('function renderAgentManagementControl()')),context);
 const form=vm.runInContext('renderAgentManagementControl()',context),inputs=walk(form).filter(n=>n.tag==='input');assert.equal(inputs.length,4);
 assert.deepEqual(inputs.map(x=>[x.name,x.checked]),[['assistantPlugins',false],['assistantReferences',true],['aidePlugins',true],['aideReferences',false]]);
 inputs[0].checked=true;await walk(form).find(n=>n.tag==='button').onclick();assert.deepEqual(JSON.parse(calls[0][1].body),{agentManagement:{assistantPlugins:true,assistantReferences:true,aidePlugins:true,aideReferences:false}});
 // Exercise the real proposal UI: code is text only; confirmation requires opening the preview.
 const proposal={id:'test',actor:'assistant',kind:'plugins',action:'prepare',target:'fixture',reason:'fixture',status:'pending'};
 const ui=vm.createContext({state,lockScreen:{locked:false},t:x=>x,window:{},document:{hidden:false,createElement:tag=>new Element(tag),getElementById:id=>id==='management-proposals'?wrap:null,querySelector:()=>new Element('div'),addEventListener(){}},setInterval(){},setTimeout(){},URL,toast(){},api:async path=>path.endsWith('/test')?{manifest:{name:'<script>not executable</script>'},files:{'index.js':'globalThis.MUST_NOT_RUN=true'}}:{proposals:[proposal]}});
 vm.runInContext(fs.readFileSync('internal/server/web/management.js','utf8'),ui);await new Promise(r=>setImmediate(r));
 let confirm=walk(wrap).find(n=>n.tag==='button'&&n.textContent==='确认执行');assert.ok(confirm.disabled);
 const details=walk(wrap).find(n=>n.tag==='details');details.open=true;await details.ontoggle();assert.equal(confirm.disabled,false);assert.equal(ui.MUST_NOT_RUN,undefined);
 assert.ok(walk(wrap).some(n=>n.tag==='pre'&&n.textContent.includes('globalThis.MUST_NOT_RUN')));
 ui.lockScreen.locked=true;await ui.window.AideManagement.refresh();assert.equal(wrap.childNodes.length,0);
 console.log('Management settings and proposal review gate passed');
})().catch(e=>{console.error(e);process.exitCode=1});
