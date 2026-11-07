'use strict';
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const schema=JSON.parse(fs.readFileSync('internal/server/web/settings-schema.json','utf8'));
const context=vm.createContext({window:{}});
vm.runInContext(fs.readFileSync('internal/server/web/settings-navigation.js','utf8'),context);
const navigation=context.window.AideSettingsNavigation;
test('all settings remain reachable exactly once in seven ordered groups',()=>{
 const model=navigation.build(schema),ids=model.groups.flatMap(group=>group.pages.map(page=>page.id));
 assert.deepEqual(model.groups.map(group=>group.id),['general','files','ai','assistant','communication','data','system']);
 assert.equal(ids.length,model.pages.length);assert.equal(new Set(ids).size,ids.length);
 for(const section of schema.sections){
  const page=navigation.resolve(model,section.id,'');assert.equal(page.sectionId,section.id);
  for(const child of section.children||[])assert.equal(navigation.resolve(model,section.id,child.id).id,child.id);
 }
 assert.equal(model.groups.find(group=>group.id==='system').pages.find(page=>page.id==='interface-tests').sectionId,'accessibility');
 assert.equal(model.groups.find(group=>group.id==='general').pages.some(page=>page.id==='interface-tests'),false);
 assert.deepEqual(model.groups.find(group=>group.id==='ai').pages.map(page=>page.id),['model-profiles','execution-policy','permissions','agent-management','computer-control']);
});
test('search finds pages by title and control while retaining group mapping',()=>{
 const model=navigation.build(schema);
 assert.deepEqual(navigation.search(model,'电脑控制').map(item=>item.page.id),['computer-control']);
 assert.deepEqual(navigation.search(model,'日志级别').map(item=>item.page.id),[]);
 assert.deepEqual(navigation.search(model,'系统日志').map(item=>item.page.id),['system-logs']);
 assert.deepEqual(navigation.search(model,'沙箱模式').map(item=>item.page.id),['permissions']);
 assert.equal(navigation.search(model,'  ').length,0);
});
test('every schema control has a renderer and key API routes exist',()=>{
 const app=fs.readFileSync('internal/server/web/app.js','utf8');
 const server=fs.readFileSync('internal/server/server.go','utf8');
 for(const section of schema.sections)for(const page of section.children||[section])for(const control of page.controls||[]){
  const type=control.type;
  assert(type,`missing type: ${control.id}`);
  assert(app.includes(`'${type}':`)||app.includes(`"${type}":`)||app.includes(`${type}:`)||app.includes(`controlRenderers['${type}']`),`missing renderer: ${type} (${control.id})`);
 }
 for(const path of ['/api/settings','/api/computer-control/status','/api/system-logs','/api/updates/slots','/api/profiles']){
  assert(server.includes(path),`server route missing: ${path}`);
  assert(app.includes(path.replace('/api','')),`UI mapping missing: ${path}`);
 }
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
 assert.deepEqual(controls.find(c=>c.id==='palette').options.map(x=>x.value),['comfort','green','professional','modern']);
 assert.deepEqual(controls.find(c=>c.id==='theme').options.map(x=>x.value),['light','dark','system']);
 const app=fs.readFileSync('internal/server/web/app.js','utf8');
 assert(!app.includes('前往模型设置配置厂商与密钥'));
});
test('modern palette persists and immediately changes the document theme attribute',()=>{
 const values={'aide.ui':JSON.stringify({palette:'modern',paletteVersion:3,theme:'dark'})},attributes={},listeners={};
 const browser={localStorage:{getItem:k=>values[k]??null,setItem:(k,v)=>values[k]=v,removeItem:k=>delete values[k]},navigator:{language:'zh-CN'},dispatchEvent(){},matchMedia:q=>({matches:q.includes('color-scheme: dark'),addEventListener(){}}),addEventListener:(n,fn)=>listeners[n]=fn};
 vm.runInNewContext(fs.readFileSync('internal/server/web/settings-init.js','utf8'),{window:browser,document:{documentElement:{setAttribute:(k,v)=>attributes[k]=v}},Event:class{}});
 assert.equal(attributes['data-palette'],'modern');assert.equal(attributes['data-theme'],'dark');
 browser.aideUI.set('palette','comfort');assert.equal(attributes['data-palette'],'comfort');assert.equal(browser.aideUI.get('palette'),'comfort');
 assert.equal(browser.aideUI.set('background','modern'),'modern');assert.equal(browser.aideUI.get('background'),'modern');
 listeners.storage({key:'aide.ui',newValue:JSON.stringify({palette:'modern',paletteVersion:3,theme:'light',background:'modern'})});
 assert.equal(attributes['data-palette'],'modern');assert.equal(attributes['data-theme'],'light');assert.equal(browser.aideUI.get('background'),'modern');
 const legacyBlue={'aide.ui':JSON.stringify({palette:'blue',paletteVersion:2,theme:'dark'})},legacyAttributes={};
 const legacyBrowser={localStorage:{getItem:k=>legacyBlue[k]??null,setItem:(k,v)=>legacyBlue[k]=v,removeItem:k=>delete legacyBlue[k]},navigator:{language:'zh-CN'},dispatchEvent(){},matchMedia:q=>({matches:q.includes('color-scheme: dark'),addEventListener(){}}),addEventListener(){}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/settings-init.js','utf8'),{window:legacyBrowser,document:{documentElement:{setAttribute:(k,v)=>legacyAttributes[k]=v}},Event:class{}});
 assert.equal(legacyAttributes['data-palette'],'modern');assert.equal(legacyBrowser.aideUI.get('palette'),'modern');
});
test('opening voice settings never requests microphone permission; explicit refresh stops its temporary track',async()=>{
 const source=fs.readFileSync('internal/server/web/app.js','utf8');let permissions=0,stopped=0;
 function el(tag,className,text){return {tag,className,text,children:[],append(...nodes){this.children.push(...nodes)},replaceChildren(){this.children=[]}};}
 const ctx=vm.createContext({el,t:x=>x,state:{config:{}},action:fn=>fn,navigator:{mediaDevices:{enumerateDevices:async()=>[{kind:'audioinput',deviceId:'mic',label:permissions?'Microphone':''}],getUserMedia:async()=>{permissions++;return {getTracks:()=>[{stop(){stopped++}}]}}}}});
 vm.runInContext(source.slice(source.indexOf('function renderVoiceInputSourceControl()'),source.indexOf("controlRenderers['voice-input-source']")),ctx);
 const control=ctx.renderVoiceInputSourceControl();await new Promise(resolve=>setImmediate(resolve));assert.equal(permissions,0);
 const button=control.children[1].children[1];assert.equal(button.text,'授权并刷新设备');await button.onclick();assert.equal(permissions,1);assert.equal(stopped,1);
});
