const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/parallel-chat.js','utf8');
function boot(stored,throws=false){
 const writes=[];const context={window:{},localStorage:{getItem(){if(throws)throw Error('blocked');return stored;},setItem(k,v){if(throws)throw Error('blocked');writes.push([k,v]);}}};vm.createContext(context);vm.runInContext(source,context);return {api:context.window.AideParallelChat,writes};
}
const t=(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n]);
for(const value of [null,'0','6','7','8','9','11','100','2.5','01','evil'])assert.equal(boot(value).api.value('chat',false),1);
const {api,writes}=boot('3');assert.equal(api.value('chat',false),3);assert.equal(api.value('workflow',false),1);assert.equal(api.value('chat',true),1);
let mode='chat',assistant=false;const select={addEventListener(name,fn){this[name]=fn;}};api.install({select,mode:()=>mode,assistant:()=>assistant,t});assert.equal(select.value,'3');select.value='5';select.change();assert.equal(api.value('chat',false),5);assert.equal(writes.at(-1)[1],'5');mode='workflow';api.sync();assert.equal(select.disabled,true);assert.match(select.title,/普通对话/);mode='chat';assistant=true;api.sync();assert.equal(select.disabled,true);
const denied=boot(null,true).api;const s={addEventListener(name,fn){this[name]=fn;}};denied.install({select:s,mode:()=> 'chat',assistant:()=>false,t});s.value='4';s.change();assert.equal(denied.value('chat',false),4,'storage errors must not prevent in-tab selection');
const el=(tag,cls,text)=>({tag,className:cls,textContent:text,children:[],dataset:{},append(...items){this.children.push(...items);}});
const run={id:'run-one',parallelism:5,parallel:{phase:'merging',parts:[{title:'<img onerror=alert(1)>',status:'completed'},{title:'missing',status:'failed'}]}};
const panel=api.render(run,{el,t,open:new Set(['run-one:parallel'])});assert.equal(panel.open,true);assert.match(panel.children[0].textContent,/并行 5x · 汇总回答 · 2\/2/);assert.equal(panel.children[1].children[0].textContent,'<img onerror=alert(1)>');assert.equal(panel.children[1].children[0].children.length,0,'untrusted planner titles stay literal');assert.equal(api.render({id:'other',parallelism:1},{el,t}),null);
const app=fs.readFileSync('internal/server/web/app.js','utf8');const start=app.indexOf('function sessionRevision('),end=app.indexOf('function getSessionWindow(',start);const c={state:{historyLimit:30}};vm.createContext(c);vm.runInContext(app.slice(start,end),c);const sess={id:'session',runs:[run],messages:[]};const before=c.sessionRevision(sess);run.parallel.phase='completed';assert.notEqual(c.sessionRevision(sess),before,'progress must repaint even without new answer text or timestamps');
console.log('parallel chat PASS: 1-5/10 selection, persistence denial, mode gates, safe progress, session isolation and progress revision');

const activeRun={id:'active',parallelism:5,parallel:{phase:'planning'}};
assert.equal(api.render(activeRun,{el,t,initial:true}).open,true,'first plan must be visible');
assert.equal(api.render(activeRun,{el,t,initial:false}).open,false,'respect user collapse after initial render');
assert.match(app,/addEventListener\('parallel', \(\) => \{ if \(state.stream === es\) refreshSessionSoon\(\);/);

// Menu rows use the same persisted preference as task submission.
const choices=boot('3');choices.api.set('5');assert.equal(choices.api.value('chat',false),5);assert.equal(choices.writes.at(-1)[1],'5');assert.equal(choices.api.value('workflow',false),1);choices.api.set('1');assert.equal(choices.api.value('chat',false),1);
console.log('parallel menu choice PASS: setting rows persist the same request limit and preserve mode gates');

assert.equal(JSON.stringify(api.choices),JSON.stringify([[1,'标准速度'],[2,'轻度'],[3,'中度'],[4,'高'],[5,'极高'],[10,'极致']]));
choices.api.set('10');assert.equal(choices.api.value('chat',false),10);assert.equal(boot(choices.writes.at(-1)[1]).api.value('chat',false),10,'10x survives reload');
assert.match(app,/for \(const \[value, description\] of window.AideParallelChat.choices\)/,'visible menu uses shared choices');
const html=fs.readFileSync('internal/server/web/index.html','utf8');assert.match(html,/<option value="10">10x<\/option>/,'hidden select must retain 10x during sync');
console.log('parallel 10x PASS: exact requested labels, persisted 10, invalid gaps rejected and visible/hidden controls agree');
