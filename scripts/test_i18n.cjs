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
assert.equal(api.t('unknown 中文'),'unknown 中文');
ctx.window.aideEnglish['test {0}']='value {0}';
assert.equal(api.t('test {0}','中文 <script>'),'value 中文 <script>');
const input={id:'语言',title:'语言',options:[{value:'语言',label:'语言'}]};
const output=api.schema(input);
assert.equal(output.id,'语言');assert.equal(output.title,'Language');
assert.equal(output.options[0].value,'语言');assert.equal(input.title,'语言');
pref='zh-CN';assert.equal(api.t('语言'),'语言');
pref='invalid';ctx.navigator.language='zh-CN';assert.equal(api.language(),'zh-CN');
for(const [key,value] of Object.entries(ctx.window.aideEnglish)) {
 const slots=s=>[...s.matchAll(/\{\d+\}/g)].map(x=>x[0]).sort();
 assert.deepEqual(slots(value),slots(key),key);
}
const source=fs.readFileSync('internal/server/web/app.js','utf8');
const start=source.indexOf('function browseDirFromValue(value) {');
vm.runInContext(source.slice(start,source.indexOf('async function resolveExistingDir',start)), Object.assign(ctx,{state:{config:{hostLocal:'/Users/me/aide'}}}));
for(const [value,want] of [['/Users/me/aide/docs','docs'],['/Users/me/aide-other/docs','.'],['/local/docs','docs'],['~/docs','docs'],['/local/../secret','.'],['/local/docs/../internal','internal']]) assert.equal(ctx.browseDirFromValue(value),want);
console.log('PASS: localization, placeholder integrity, schema ownership, locale fallback and path mapping');
