'use strict';
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
let pref = 'en';
const ctx = {navigator:{language:'en-US'},document:{documentElement:{},querySelectorAll:()=>[]},CustomEvent:class{}, window:{aideUI:{get:()=>pref,subscribe:()=>{}},addEventListener:()=>{},dispatchEvent:()=>{}}};
vm.createContext(ctx);
for(const f of ['locales/en.js','i18n.js']) vm.runInContext(fs.readFileSync('internal/server/web/'+f,'utf8'),ctx);
const api=ctx.window.aideI18n;
assert.equal(api.language(),'en');
assert.equal(api.t('语言'),'Language');
assert.equal(api.t('显示'),'Show');
assert.equal(api.t('隐藏'),'Hide');
assert.equal(api.t('对{0}说点什么…', '小秘'),'Say something to 小秘…');
assert.equal(api.t('unknown 中文'),'unknown 中文');
ctx.window.aideEnglish['test {0}']='value {0}';
assert.equal(api.t('test {0}','中文 <script>'),'value 中文 <script>');
const input={id:'语言',title:'语言',options:[{value:'语言',label:'语言'}]};
const output=api.schema(input);
assert.equal(output.id,'语言');assert.equal(output.title,'Language');
assert.equal(output.options[0].value,'语言');assert.equal(input.title,'语言');
assert.equal(api.sourceName({id:'workspace',name:'工作区'}),'Workspace');
assert.equal(api.sourceName({id:'project-ref',name:'工作区'}),'工作区');
assert.equal(api.sourceName({id:'workspace',name:'My workspace'}),'My workspace');
const createUiContext = require('./fixtures/ui-i18n-context.cjs');
const englishBridge = createUiContext(vm, {window:{aideI18n:api}});
assert.equal(vm.runInContext("uiTextStarmap('找到 {0} 个节点', 2)", englishBridge), 'Found 2 nodes');
const fallbackBridge = createUiContext(vm, {});
assert.equal(vm.runInContext("uiTextStarmap('找到 {0} 个节点', 0)", fallbackBridge), '找到 0 个节点');
pref='zh-CN';assert.equal(api.t('语言'),'语言');
assert.equal(api.t('对{0}说点什么…', '小鲸'),'对小鲸说点什么…');
pref='invalid';ctx.navigator.language='zh-CN';assert.equal(api.language(),'zh-CN');
for(const [key,value] of Object.entries(ctx.window.aideEnglish)) {
 const slots=s=>[...s.matchAll(/\{\d+\}/g)].map(x=>x[0]).sort();
 assert.deepEqual(slots(value),slots(key),key);
}
const source=fs.readFileSync('internal/server/web/app.js','utf8');
const start=source.indexOf('function browseDirFromValue(');
vm.runInContext(source.slice(start,source.indexOf('async function resolveExistingDir',start)), Object.assign(ctx,{t:x=>x,state:{config:{hostLocal:'/Users/me/aide'}}}));
for(const [value,want] of [['/Users/me/aide/docs','docs'],['/local/docs','docs'],['~/docs','docs'],['/local/../secret','.'],['/local/docs/../internal','internal']]) assert.equal(ctx.browseDirFromValue(value),want);
ctx.state.config.hostLocal='C:\\Users\\me';
for(const [value,want] of [['C:\\Users\\me\\docs','docs'],['C:/Users/me/docs','docs'],['c:\\users\\ME\\docs','docs']]) assert.equal(ctx.browseDirFromValue(value),want);
console.log('PASS: localization, placeholder integrity, schema ownership, locale fallback and path mapping');

assert.throws(()=>ctx.browseDirFromValue('/Users/me/aide-other/docs'));
ctx.state.config.hostLocal='/';
assert.equal(ctx.browseDirFromValue('/Users/me/Project'),'Users/me/Project');

ctx.state.localMounts=[{id:'archive',path:'E:/资料'},{id:'ssd',path:'/Volumes/SSD/Projects'}];
assert.equal(ctx.browseDirFromValue('e:/资料/子目录','mount:archive'),'子目录');
assert.equal(ctx.browseDirFromValue('/Volumes/SSD/Projects/sub','mount:ssd'),'sub');
assert.throws(()=>ctx.browseDirFromValue('F:/资料/sub','mount:archive'));
assert.throws(()=>ctx.browseDirFromValue('/Volumes/SSD/Projects2/sub','mount:ssd'));

// Product status/actions translate; workflow and node names remain user content.
pref='en';
class WorkflowNode {
 constructor(tag, cls, text='') { this.tag=tag;this.textContent=text;this.children=[];this.style={}; }
 append(...nodes) { this.children.push(...nodes); }
}
const workflowContext=vm.createContext({t:api.t,el:(...args)=>new WorkflowNode(...args),action:fn=>fn});
const workflowStart=source.indexOf('function renderPWRun(');
vm.runInContext(source.slice(workflowStart,source.indexOf('\n}',workflowStart)+2),workflowContext);
const workflow=workflowContext.renderPWRun({status:'running',pw:{definition:{name:'用户流程名',nodes:[{id:'a',title:'用户节点'},{id:'b',title:'Done node'},{id:'c',title:'Skip node'}]},nodes:{a:{status:'waiting'},b:{status:'completed'},c:{status:'skipped'}}}});
assert.equal(workflow.children[0].textContent,'PW · 用户流程名');
assert.equal(workflow.children[1].children[0].textContent,'用户节点 · Awaiting confirmation');
assert.equal(workflow.children[2].children[0].textContent,'Done node · Succeeded');
assert.equal(workflow.children[3].children[0].textContent,'Skip node · Skipped');
assert.deepEqual(workflow.children[1].children.filter(n=>n.tag==='button').map(n=>n.textContent),['Confirm action','Reject']);
console.log('PASS: workflow action/status localization preserves user names');
