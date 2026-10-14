const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const helper=source.slice(source.indexOf('function cosmicEmptyMessage('),source.indexOf('function cosmicData('));
const c=vm.createContext({region:'source:test',sourceByRegion:new Map(),sourceCatalog:[],loading:false,graph:{nodes:[]}});vm.runInContext(helper,c);
for(const [descriptor,pattern] of [[{state:'disabled'},/停用/],[{state:'unavailable',detached:true},/未报告/],[{state:'unavailable',coverage:{pending:8}},/暂不可用/],[{state:'partial',coverage:{pending:8}},/分批扫描/],[{state:'ready',coverage:{refreshProgress:true}},/分批扫描/],[{state:'ready'},/检查目录/]]){
 c.sourceByRegion.set(c.region,descriptor);assert.match(c.cosmicEmptyMessage(),pattern);
}
c.sourceByRegion.clear();c.region='';assert.match(c.cosmicEmptyMessage(),/添加引用/);c.loading=true;assert.match(c.cosmicEmptyMessage(),/分批扫描/);c.loading=false;c.graph.nodes=[{id:'one',region:'workspace'}];assert.match(c.cosmicEmptyMessage(),/调整聚焦/);
const label=source.match(/if\(!cosmicScene.members.length\)\{[^\n]+\}/)?.[0];assert.ok(label);
for(const width of [320,390,1324]){const calls=[];const ctx={fillText:(...args)=>calls.push(args)};const v=vm.createContext({cosmicScene:{members:[]},ctx,skyText:()=>'',cosmicEmptyMessage:()=> '来源暂不可用，请在星际日志查看原因',width,cx:width*.57,cy:300});vm.runInContext(label,v);assert.equal(ctx.textAlign,'center');assert.equal(calls.length,1);const [,x,,max]=calls[0];assert.ok(x-max/2>=16-1e-9&&x+max/2<=width-16+1e-9);}
const branch=source.match(/if\(!changed.length&&!removed.size&&!edgesChanged\)\{[^\n]+\}/)?.[0];assert.ok(branch);
for(const members of [[],['one']]){let redraws=0;const v=vm.createContext({changed:[],removed:new Set(),edgesChanged:false,update:{stale:false},cosmicScene:{members},redraw:()=>redraws++});vm.runInContext('(function(){'+branch+'})()',v);assert.equal(redraws,members.length?0:1);}
console.log('PASS source empty/failure/scanning states, bounded labels and empty-scene metadata repaint');

const catalog=vm.createContext({window:{}});vm.runInContext(fs.readFileSync('internal/server/web/locales/en.js','utf8'),catalog);
c.graph.nodes=[];c.aideI18n={t:key=>catalog.window.aideEnglish[key]||key};assert.equal(c.cosmicEmptyMessage(),'No nodes found. Add a reference or check the workspace.');
