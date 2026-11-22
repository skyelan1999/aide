'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
const fn=source.slice(source.indexOf('function composerModelStatus('),source.indexOf('function refreshStrategyUI('));
const state={session:null,config:{configured:true,model:'global',models:[{id:'global',name:'Global',providerId:'a'},{id:'mimo',name:'MiMo',providerId:'b'}],modelRouter:{available:true}},xiaomiModelSettings:{source:'configured',model:'mimo',effectiveModel:'mimo',configured:true}};
const context={state,modelProviderName:id=>({a:'Provider A',b:'Provider B'})[id]||''};vm.createContext(context);vm.runInContext(fn,context);
const show=choice=>JSON.parse(JSON.stringify(context.composerModelStatus(choice)));
assert.equal(show({model:'global'}).label,'Global · Provider A');
state.session={kind:'assistant'};let s=show({model:'global',modelRouter:'auto'});assert.equal(s.model,'mimo');assert.equal(s.label,'MiMo · Provider B');assert.equal(s.auto,false);assert.equal(s.configured,true);
for(const source of ['inherit','configured','custom']) {state.xiaomiModelSettings={source,effectiveModel:'global',configured:true};assert.equal(show({model:'mimo'}).label,'Global · Provider A');}
state.xiaomiModelSettings={source:'configured',effectiveModel:'removed',configured:false};assert.equal(show({model:'global'}).model,'removed');assert.equal(show({model:'global'}).configured,false);
state.xiaomiModelSettings=null;assert.equal(show({model:'global'}).model,'');assert.equal(show({model:'global'}).configured,false);
state.session=null;assert.equal(show({model:'global',modelRouter:'auto'}).label,'Auto');assert.equal(show({model:'global'}).model,'global');
const render=source.slice(source.indexOf('function renderSession()'),source.indexOf('function renderSession()')+1000);assert(render.indexOf("classList.toggle('assistant-mode'")<render.indexOf('refreshStrategyUI()'));
const refresh=source.slice(source.indexOf('function refreshStrategyUI()'),source.indexOf('function refreshStrategyUI()')+2000);assert.match(refresh,/display\.label/);assert.match(refresh,/display\.configured/);
console.log('PASS assistant/global/custom/fallback/missing model display and mode-switch ordering');
