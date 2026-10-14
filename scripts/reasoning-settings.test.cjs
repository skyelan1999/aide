const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/reasoning-settings.js','utf8');
function setup(){const calls=[],messages=[],select={value:'',replaceChildren(...xs){this.options=xs}},button={disabled:false};let cfg={reasoningEffort:'auto'};let refreshError=false;let deferred;
 const context={window:{},Option:class{constructor(label,value){this.label=label;this.value=value}}};vm.createContext(context);vm.runInContext(source,context);
 const api=async(path,options)=>{calls.push({path,...options});if(deferred)await deferred;cfg={reasoningEffort:JSON.parse(options.body).reasoningEffort}};
 const module=context.window.AideReasoningSettings.install({select,button,api,refresh:async()=>{if(refreshError)throw Error('offline')},config:()=>cfg,t:(s,arg)=>s.replace('{0}',arg),toast:s=>messages.push(s)});module.sync();
 return {select,button,calls,messages,module,setConfig:v=>cfg=v,setError:v=>refreshError=v,setWait:v=>deferred=v};}
(async()=>{const f=setup();assert.equal(f.select.options.length,5);assert.equal(f.select.value,'auto');
 for(const value of ['high','medium','low','off','auto']){f.select.value=value;await f.button.onclick();assert.deepEqual(JSON.parse(f.calls.at(-1).body),{reasoningEffort:value});assert.equal(f.button.disabled,false);assert.match(f.messages.at(-1),/已保存/)}
 let release;f.setWait(new Promise(r=>release=r));f.select.value='high';const first=f.button.onclick();assert.equal(f.button.disabled,true);const count=f.calls.length;await f.button.onclick();assert.equal(f.calls.length,count,'double click writes once');f.select.value='low';release();await first;assert.equal(f.select.value,'low','saved result preserves newer unsaved selection');
 f.setWait(null);f.setError(true);await f.button.onclick();assert.match(f.messages.at(-1),/offline/);assert.equal(f.button.disabled,false);f.setError(false);await f.button.onclick();assert.match(f.messages.at(-1),/已保存/);
 f.select.value='unknown';await f.button.onclick();assert.match(f.messages.at(-1),/无效/);f.setConfig({reasoningEffort:'high'});f.module.sync();assert.equal(f.select.value,'high');
 const html=fs.readFileSync('internal/server/web/index.html','utf8'),app=fs.readFileSync('internal/server/web/app.js','utf8');assert.match(html,/type="button" id="save-default-reasoning"/);assert.match(app,/defaultReasoningSettings.sync\(\)/);assert.ok(html.indexOf('/reasoning-settings.js?')<html.indexOf('/app.js?'));
 console.log('reasoning settings PASS: independent payload, five values, double click, pending edit, retry, refresh and pre-model controls');
})().catch(error=>{console.error(error);process.exitCode=1});
