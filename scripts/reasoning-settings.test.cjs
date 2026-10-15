const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const html=fs.readFileSync('internal/server/web/index.html','utf8'),app=fs.readFileSync('internal/server/web/app.js','utf8');
const dialog=html.match(/<dialog id="settings-dialog">[\s\S]*?<\/dialog>/)[0];
assert.doesNotMatch(dialog,/model-reasoning-effort|save-default-reasoning/,'model settings has no duplicate reasoning controls');
assert.doesNotMatch(app,/defaultReasoningSettings|model-reasoning-effort/);
assert.doesNotMatch(html,/src="\/reasoning-settings\.js/);
const source=app.slice(app.indexOf("$('settings-form').onsubmit ="),app.indexOf('// 虚拟形象：aide'));
async function check(reasoningEffort){
 const calls=[],form={},dialog={close(){this.closed=true}},messages=[];
 const config={reasoningEffort,activeModel:'old'};
 const draft={models:[{id:'test-model',providerId:'test',name:'Test'}],providers:[{id:'test',name:'Test',baseURL:' https://example.invalid/v1 ',keys:[{id:'key',label:'Test'}],activeKeyId:'key'}],activeModel:'test-model',providerSecrets:new Map([['test:key','fixture-secret']])};
 const context={state:{config,modelDraft:draft},$:(id)=>({'settings-form':form,'settings-dialog':dialog}[id]),action:fn=>fn,api:async(path,options)=>calls.push({path,body:JSON.parse(options.body)}),refreshConfig:async()=>{},toast:s=>messages.push(s),t:s=>s};
 vm.createContext(context);vm.runInContext(source,context);
 await form.onsubmit({preventDefault(){}});
 assert.equal(calls.length,1);assert.equal(calls[0].path,'/settings');
 assert.ok(!Object.hasOwn(calls[0].body,'reasoningEffort'),'saving models omits reasoning to preserve existing default');
 assert.equal(calls[0].body.activeModel,'test-model');assert.equal(calls[0].body.models[0].id,'test-model');
 assert.equal(calls[0].body.providerKeyWrites[0].apiKey,'fixture-secret');assert.equal(config.reasoningEffort,reasoningEffort);
 assert.equal(dialog.closed,true);assert.equal(messages.length,1);
}
(async()=>{for(const effort of ['auto','off','low','medium','high'])await check(effort);console.log('model settings PASS: no duplicate controls; actual submit preserves reasoning and provider/model/key payloads');})().catch(error=>{console.error(error);process.exitCode=1});
