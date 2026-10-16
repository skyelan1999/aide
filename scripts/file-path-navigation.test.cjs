'use strict';
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const code = source.slice(source.indexOf('function fileListRequestKey()'), source.indexOf('function currentFileTarget()'));
function fixture(fetch) {
  const nodes = Object.fromEntries(['file-path', 'new-file', 'files', 'login-dialog'].map(id => [id, {title:'', value:'', showModal(){this.open=true;}}]));
  const state = {root:'workspace', source:'', dir:'.', sources:[], fileDirs:{}, fileSearch:'', fileSearchScope:'folder', fileSearchMatch:'fuzzy', fileEntries:[{path:'keep.md'}], fileSelection:new Set(['keep.md'])};
  const document = {activeElement:nodes['file-path'], querySelector:()=>null};
  const c = {state, document, fetch, $:id=>nodes[id], recoveryScope:()=> 'workspace1', t:s=>s, currentFileTarget:()=>({writable:true}), renderFileEntries(){}, syncFileSelection(){}, scheduleChatRecovery(){}};
  vm.createContext(c); vm.runInContext(code,c); c.updateFilePathLabel();
  return {c,state,nodes};
}
const response = (status, data) => ({ok:status===200,status,json:async()=>data,headers:{get:()=>null}});
(async()=>{
  const urls=[]; const f=fixture(async url=>{urls.push(url);return response(200,[{path:'sub/code.js'}]);});
  await f.c.navigateFilePath('/workspace/sub');
  assert.equal(f.state.dir,'sub'); assert.equal(f.nodes['file-path'].value,'/workspace/sub');
  assert.ok(urls[0].includes('path=sub'));
  assert.equal(f.c.resolveFilePath('../other/./nested/'),'other/nested');
  assert.equal(f.c.resolveFilePath('/workspace'),'.');
  assert.throws(()=>f.c.resolveFilePath('/etc'),/当前文件来源/);
  assert.throws(()=>f.c.resolveFilePath('../../outside'),/当前文件来源/);
  f.nodes['file-path'].value='typing';f.c.updateFilePathLabel();assert.equal(f.nodes['file-path'].value,'typing','refresh preserves editing');
  f.c.updateFilePathLabel(true);assert.equal(f.nodes['file-path'].value,'/workspace/sub');
  f.c.fetch=async()=>response(404,{error:'missing'});
  await assert.rejects(f.c.navigateFilePath('/workspace/missing'),/missing/);
  assert.equal(f.state.dir,'sub');assert.equal(f.state.fileEntries[0].path,'sub/code.js');assert.equal(f.state.fileListError,false);
  f.state.root='context';f.state.source='docs';f.state.sources=[{id:'docs',name:'My Docs'}];
  assert.equal(f.c.resolveFilePath('/sources/My Docs/a b'),'a b');
  assert.throws(()=>f.c.resolveFilePath('/workspace/sub'),/当前文件来源/);
  let finish;const stale=fixture(()=>new Promise(resolve=>finish=resolve));
  const pending=stale.c.navigateFilePath('/workspace/old');stale.state.source='other';stale.state.root='context';finish(response(200,[]));await pending;
  assert.equal(stale.state.dir,'.');assert.equal(stale.state.fileEntries[0].path,'keep.md');
  const jobs=[];const race=fixture(()=>new Promise(resolve=>jobs.push(resolve)));
  const first=race.c.navigateFilePath('/workspace/first'),second=race.c.navigateFilePath('/workspace/second');
  jobs[0](response(200,[]));await first;assert.equal(race.state.dir,'.');
  jobs[1](response(200,[]));await second;assert.equal(race.state.dir,'second');
  console.log('file path navigation: PASS (paths, confinement, failure preservation, refresh, stale source, request race)');
})().catch(error=>{console.error(error);process.exitCode=1;});
