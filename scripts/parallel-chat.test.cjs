const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/parallel-chat.js','utf8');
function boot(stored,throws=false){
 const writes=[];const context={window:{},localStorage:{getItem(){if(throws)throw Error('blocked');return stored;},setItem(k,v){if(throws)throw Error('blocked');writes.push([k,v]);}}};vm.createContext(context);vm.runInContext(source,context);return {api:context.window.AideParallelChat,writes};
}
const t=(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n]);
for(const value of [null,'0','6','2.5','evil'])assert.equal(boot(value).api.value('chat',false),1);
const {api,writes}=boot('3');assert.equal(api.value('chat',false),3);assert.equal(api.value('workflow',false),1);assert.equal(api.value('chat',true),1);
let mode='chat',assistant=false;const select={addEventListener(name,fn){this[name]=fn;}};api.install({select,mode:()=>mode,assistant:()=>assistant,t});assert.equal(select.value,'3');select.value='5';select.change();assert.equal(api.value('chat',false),5);assert.equal(writes.at(-1)[1],'5');mode='workflow';api.sync();assert.equal(select.disabled,true);assert.match(select.title,/普通对话/);mode='chat';assistant=true;api.sync();assert.equal(select.disabled,true);
const denied=boot(null,true).api;const s={addEventListener(name,fn){this[name]=fn;}};denied.install({select:s,mode:()=> 'chat',assistant:()=>false,t});s.value='4';s.change();assert.equal(denied.value('chat',false),4,'storage errors must not prevent in-tab selection');
const el=(tag,cls,text)=>({tag,className:cls,textContent:text,children:[],dataset:{},append(...items){this.children.push(...items);}});
const run={id:'run-one',parallelism:5,parallel:{phase:'merging',parts:[{title:'<img onerror=alert(1)>',status:'completed'},{title:'missing',status:'failed'}]}};
const panel=api.render(run,{el,t,open:new Set(['run-one:parallel'])});assert.equal(panel.open,true);assert.match(panel.children[0].textContent,/并行 5x · 汇总回答 · 2\/2/);assert.equal(panel.children[1].children[0].textContent,'<img onerror=alert(1)>');assert.equal(panel.children[1].children[0].children.length,0,'untrusted planner titles stay literal');assert.equal(api.render({id:'other',parallelism:1},{el,t}),null);
const app=fs.readFileSync('internal/server/web/app.js','utf8');const start=app.indexOf('function sessionRevision('),end=app.indexOf('function getSessionWindow(',start);const c={state:{historyLimit:30}};vm.createContext(c);vm.runInContext(app.slice(start,end),c);const sess={id:'session',runs:[run],messages:[]};const before=c.sessionRevision(sess);run.parallel.phase='completed';assert.notEqual(c.sessionRevision(sess),before,'progress must repaint even without new answer text or timestamps');
console.log('parallel chat PASS: 1-5 selection, persistence denial, mode gates, safe progress, session isolation and progress revision');

const activeRun={id:'active',parallelism:5,parallel:{phase:'planning'}};
assert.equal(api.render(activeRun,{el,t,initial:true}).open,true,'first plan must be visible');
assert.equal(api.render(activeRun,{el,t,initial:false}).open,false,'respect user collapse after initial render');
assert.match(app,/addEventListener\('parallel', \(\) => \{ if \(state.stream === es\) refreshSessionSoon\(\);/);

// The wheel uses the existing selected value across hidden menu geometry,
// keyboard navigation, scrolling, and mode changes.
function wheelNode(){return {children:[],attrs:{},listeners:{},classList:{toggle(){}},offsetHeight:28,clientHeight:84,scrollTop:0,append(child){this.children.push(child)},setAttribute(k,v){this.attrs[k]=v},addEventListener(k,fn){this.listeners[k]=fn},focus(){this.focused=true}}}
const wheelBoot=boot('4'),control=wheelNode();control.id='speed';control.ownerDocument={createElement:wheelNode};control.after=node=>{control.wheel=node};let wheelMode='chat';
wheelBoot.api.install({select:control,mode:()=>wheelMode,assistant:()=>false,t});
const list=control.wheel.children[0],press=key=>list.listeners.keydown({key,preventDefault(){}});
assert.equal(control.hidden,true);assert.equal(list.scrollTop,84);assert.equal(list.attrs['aria-activedescendant'],'speed-option-4');
press('End');assert.equal(control.value,'5');press('ArrowDown');assert.equal(control.value,'5');press('Home');assert.equal(control.value,'1');press('ArrowDown');assert.equal(control.value,'2');
list.scrollTop=56;list.listeners.scroll();assert.equal(control.value,'3');assert.equal(wheelBoot.writes.at(-1)[1],'3');
list.scrollTop=200;list.listeners.scroll();assert.equal(control.value,'5','bottom overscroll must clamp, never wrap back to 1x');
list.clientHeight=0;list.scrollTop=0;list.listeners.scroll();wheelBoot.api.sync();assert.equal(control.value,'5','closing/hidden menus must not overwrite the choice with zero geometry');
list.clientHeight=84;wheelBoot.api.sync();assert.equal(list.scrollTop,112,'opening a rebuilt menu restores the selected wheel position');
wheelMode='workflow';wheelBoot.api.sync();press('Home');assert.equal(control.value,'5');assert.equal(list.attrs['aria-disabled'],'true');assert.equal(list.tabIndex,-1);
console.log('parallel wheel PASS: scroll, keyboard, end clamps, hidden menu preservation and disabled modes');
