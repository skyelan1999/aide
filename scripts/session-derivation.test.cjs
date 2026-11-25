const assert = require('assert');
const fs = require('fs');
const path = require('path');
const vm = require('vm');
const root = path.join(__dirname, '..');
const source = fs.readFileSync(path.join(root, 'internal/server/web/app.js'), 'utf8');
const html = fs.readFileSync(path.join(root, 'internal/server/web/index.html'), 'utf8');
const code = source.slice(source.indexOf("let sessionDerivationKind = '';"), source.indexOf('async function newSession()'));
function element() {
  const classes = new Set();
  return { disabled:false, hidden:false, title:'', classList:{ toggle:(name,on)=>on?classes.add(name):classes.delete(name), contains:name=>classes.has(name) }, setAttribute(name,value){this[name]=value;}, focus(){this.focused=true;} };
}
const branch = element(), child = element(), prompt = element();
const elements = {'prompt':prompt};
const documentScope = {querySelectorAll(selector){
  if(selector==='.msg-btn-branch-session')return[branch];
  if(selector==='.msg-btn-create-subagent')return[child];
  return[];
}};
const calls = [];
const toasts = [];
const context = {
  sessionDerivationKind:'',
  document:documentScope,
  state:{session:null},
  $:id=>elements[id], t:s=>s, toast:s=>toasts.push(s), persistChatDraft(){calls.push(['draft']);},
  async api(url,options){calls.push(['api',url,JSON.parse(options.body)]);const child=JSON.parse(options.body).kind==='subagent';return{id:'derived',parentId:child?'source':''};},
  async loadSessions(){calls.push(['load']);},
  async selectSession(id){calls.push(['select',id]);context.state.session={id,parentId:context.state.session?.id==='source'&&calls.find(c=>c[0]==='api')?.[2].kind==='subagent'?'source':'',runs:[]};},
};
vm.createContext(context);
vm.runInContext(code,context);
context.syncSessionDerivationControls();
assert.equal(branch.disabled,true);assert.equal(child.disabled,true);assert.equal(child.classList.contains('hidden'),true);
context.state.session={id:'source',kind:'',runs:[]};context.syncSessionDerivationControls();
assert.equal(branch.disabled,false);assert.equal(child.disabled,false);assert.equal(child.classList.contains('hidden'),false);
(async()=>{
  await context.deriveCurrentSession('session');
  assert.deepEqual(calls.find(c=>c[0]==='api'),['api','/sessions/source/derive',{kind:'session'}]);
  assert.equal(calls.find(c=>c[0]==='select')?.[1],'derived');
  context.state.session={id:'source',kind:'',runs:[]};
  await context.deriveCurrentSession('subagent');
  assert.equal(calls.filter(c=>c[0]==='api').at(-1)[2].kind,'subagent');
  context.state.session={id:'child',parentId:'source',runs:[]};context.syncSessionDerivationControls();
  assert.equal(child.classList.contains('hidden'),true,'child agent hides nested creation');
  assert.equal(branch.disabled,false,'child can still create an independent main branch');
  const before=calls.filter(c=>c[0]==='api').length;
  await context.deriveCurrentSession('subagent');
  assert.equal(calls.filter(c=>c[0]==='api').length,before,'nested child request never reaches API');
  assert.equal(toasts.at(-1),'子 Agent 不能再次创建子 Agent');
  for (const status of ['running','awaiting_approval','awaiting_clarification','paused']) {
    context.state.session={id:'busy',runs:[{status}]};context.syncSessionDerivationControls();
    assert.equal(branch.disabled,true,status+' disables branch');assert.equal(child.disabled,true,status+' disables child');
    await context.deriveCurrentSession('session');
    assert.equal(calls.filter(c=>c[0]==='api').length,before,status+' source request never reaches API');
  }
  assert.doesNotMatch(html,/id="branch-session"|id="create-subagent"|id="session-create-actions"/);
  assert.match(source,/mk\('branch'[\s\S]*?msg-btn-branch-session/);assert.match(source,/mk\('subagent'[\s\S]*?msg-btn-create-subagent/);
  console.log('PASS: reply-action derivation buttons, context actions, busy state, and nested-agent guard');
})().catch(err=>{console.error(err);process.exit(1);});
