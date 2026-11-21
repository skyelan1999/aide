'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const app=fs.readFileSync('internal/server/web/app.js','utf8');
const constants=app.slice(app.indexOf('const FILE_DBLCLICK_MS ='),app.indexOf('function syncFileSelection()'));
const handler=app.slice(app.indexOf('    b.onclick = (ev) => {',app.indexOf('function renderFileEntries(')),app.indexOf('    b.oncontextmenu =',app.indexOf('function renderFileEntries(')));
assert.ok(handler.includes('openFileInNewTab(file)'),'exercise the real file-row handler');
function fixture(dir=false){
  let now=1000,id=0;const timers=new Map(),opened=[],tabs=[],entered=[];
  const b={_last:0,_clickTimer:0},file={path:dir?'folder':'part.step',dir};
  const state={fileSelection:new Set(),fileSelectionAnchor:-1,dir:'.'};
  const context={b,file,state,index:0,files:[file],fileNavigationGesture:{suppressClickUntil:0},
    Date:{now:()=>now},setTimeout(fn,ms){timers.set(++id,{at:now+ms,fn});return id;},clearTimeout(i){timers.delete(i);},
    document:{querySelectorAll:()=>[b]},selectFileRow(){},syncFileSelection(){},toast(){},
    openFile(path){opened.push(path);return Promise.resolve();},openFileInNewTab(f){tabs.push(f.path);},
    loadFiles(){entered.push(state.dir);return Promise.resolve();}};
  vm.createContext(context);vm.runInContext(constants+handler,context);
  function advance(ms){const end=now+ms;for(;;){const next=[...timers].filter(([,v])=>v.at<=end).sort((a,b)=>a[1].at-b[1].at)[0];if(!next)break;timers.delete(next[0]);now=next[1].at;next[1].fn();}now=end;}
  const click=(extra={})=>b.onclick({preventDefault(){},stopPropagation(){},...extra});
  return {click,advance,opened,tabs,entered,state};
}
for(const interval of [100,250,350,450]){
  const f=fixture();f.click();f.advance(interval);f.click();f.advance(1000);
  assert.equal(f.opened.length,0,`double click at ${interval}ms must not launch an in-place preview`);
  assert.deepEqual(f.tabs,['part.step']);
}
const single=fixture();single.click();single.advance(450);assert.equal(single.opened.length,0);single.advance(10);assert.deepEqual(single.opened,['part.step']);assert.equal(single.tabs.length,0);
const separate=fixture();separate.click();separate.advance(500);separate.click();separate.advance(500);assert.equal(separate.opened.length,2);assert.equal(separate.tabs.length,0);
const modified=fixture();modified.click();modified.advance(350);modified.click({ctrlKey:true});modified.advance(1000);assert.equal(modified.opened.length,0);assert.equal(modified.tabs.length,0);assert.ok(modified.state.fileSelection.has('part.step'));
const folder=fixture(true);folder.click();folder.advance(350);folder.click();folder.advance(1000);assert.deepEqual(folder.entered,['folder']);
console.log('file click open window: PASS (fast/slow/boundary double clicks, single/separate clicks, modifiers, folders)');
