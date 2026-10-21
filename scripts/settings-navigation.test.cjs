'use strict';
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const schema=JSON.parse(fs.readFileSync('internal/server/web/settings-schema.json','utf8'));
const context=vm.createContext({window:{}});
vm.runInContext(fs.readFileSync('internal/server/web/settings-navigation.js','utf8'),context);
const navigation=context.window.AideSettingsNavigation;
test('all original settings remain reachable exactly once in seven groups',()=>{
 const model=navigation.build(schema),ids=model.groups.flatMap(group=>group.pages.map(page=>page.id));
 assert.equal(model.groups.length,7);assert.equal(ids.length,model.pages.length);assert.equal(new Set(ids).size,ids.length);
 for(const section of schema.sections){
  const page=navigation.resolve(model,section.id,'');assert.equal(page.sectionId,section.id);
  for(const child of section.children||[])assert.equal(navigation.resolve(model,section.id,child.id).id,child.id);
 }
 assert.equal(model.groups.find(group=>group.id==='system').pages.find(page=>page.id==='interface-tests').sectionId,'accessibility');
 assert.equal(model.groups.find(group=>group.id==='general').pages.some(page=>page.id==='interface-tests'),false);
});
test('new sections and unknown legacy links have a safe reachable destination',()=>{
 const model=navigation.build({...schema,sections:[...schema.sections,{id:'future',title:'Future',controls:[]}]});
 assert.equal(model.groups.at(-1).pages[0].id,'future');assert.equal(navigation.resolve(model,'unknown','missing').id,'appearance');
 assert.equal(navigation.resolve(model,'model','').id,'model-profiles');
 assert.equal(navigation.resolve(model,'permissions','').id,'permissions');
});
test('advanced controls initialize once only after expansion and keep original renderer',()=>{
 const source=fs.readFileSync('internal/server/web/app.js','utf8');let renders=0;
 function el(tag,className,text){return {tag,className,text,children:[],events:{},append(...nodes){this.children.push(...nodes)},addEventListener(name,fn){this.events[name]=fn}};}
 const ctx=vm.createContext({el,t:x=>x,console,window:{},settingsPanel:{refreshers:[]},requestAnimationFrame:fn=>fn(),controlRenderers:{fixture:control=>{renders++;assert.equal(control.id,'kept');return el('input')}}});
 vm.runInContext(source.slice(source.indexOf('function renderControlsInto('),source.indexOf('function renderSettingsSheet()')),ctx);
 const host=el('section');ctx.renderControlsInto(host,[{id:'kept',type:'fixture',label:'Advanced',advanced:true}]);assert.equal(renders,0);
 const details=host.children[0];assert.equal(details.tag,'details');details.open=true;details.events.toggle();assert.equal(renders,1);details.open=false;details.events.toggle();details.open=true;details.events.toggle();assert.equal(renders,1);
});
test('appearance uses independent palette and mode with unchanged stored values',()=>{
 const controls=schema.sections.find(s=>s.id==='appearance').controls;
 assert.deepEqual(controls.filter(c=>['palette','theme'].includes(c.id)).map(c=>c.id),['palette','theme']);
 assert.deepEqual(controls.find(c=>c.id==='palette').options.map(x=>x.value),['blue','comfort','green']);
 assert.deepEqual(controls.find(c=>c.id==='theme').options.map(x=>x.value),['light','dark','system']);
});
test('opening voice settings never requests microphone permission; explicit refresh stops its temporary track',async()=>{
 const source=fs.readFileSync('internal/server/web/app.js','utf8');let permissions=0,stopped=0;
 function el(tag,className,text){return {tag,className,text,children:[],append(...nodes){this.children.push(...nodes)},replaceChildren(){this.children=[]}};}
 const ctx=vm.createContext({el,t:x=>x,state:{config:{}},action:fn=>fn,navigator:{mediaDevices:{enumerateDevices:async()=>[{kind:'audioinput',deviceId:'mic',label:permissions?'Microphone':''}],getUserMedia:async()=>{permissions++;return {getTracks:()=>[{stop(){stopped++}}]}}}}});
 vm.runInContext(source.slice(source.indexOf('function renderVoiceInputSourceControl()'),source.indexOf("controlRenderers['voice-input-source']")),ctx);
 const control=ctx.renderVoiceInputSourceControl();await new Promise(resolve=>setImmediate(resolve));assert.equal(permissions,0);
 const button=control.children[1].children[1];assert.equal(button.text,'授权并刷新设备');await button.onclick();assert.equal(permissions,1);assert.equal(stopped,1);
});
