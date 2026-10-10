const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element {
 constructor(tag){this.tagName=tag;this.children=[];this.isConnected=true;this.disabled=false;}
 append(...items){this.children.push(...items);}
 replaceChildren(...items){this.children=items;}
 click(){} setAttribute(){} querySelector(){return null;} scrollIntoView(){}
}
const descendants=root=>[root,...root.children.flatMap(descendants)];
const page=(snapshot,offset=0)=>({snapshot,taskId:'test',goal:'Fixture',status:'running',summary:{files:2,executions:2},files:[{path:'file'+offset,proposedHash:'hash'}],executions:[{id:'E'+offset,tool:'read_file',digest:'digest',preview:'return'}],verification:[],gaps:[],release:{message:'not recorded'},page:{filesMore:offset===0,executionsMore:offset===0,fileNext:offset+1,executionNext:offset+1}});
async function scenario(kind){
 let calls=[],blob,phase=0;
 const content=new Element('div');
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},Blob:class{constructor(parts){blob=JSON.parse(parts[0]);}},URL:{createObjectURL:()=>'',revokeObjectURL(){}},setTimeout(){},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const api=async path=>{calls.push(path);if(calls.length===1)return page('old');if(path.includes('snapshot=')){assert.match(path,/snapshot=old/);if(kind==='409'){const e=new Error('changed');e.status=409;throw e;}if(kind==='mismatch')return page('new',1);return page('old',1);}phase++;return page('new');};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api,t:x=>x,blocked:()=>false,error:e=>{throw new Error(e);}});
 const button=text=>descendants(content).find(e=>e.tagName==='button'&&e.textContent===text);
 await button('加载更多成果记录').onclick();
 if(kind==='stable'){
  button('导出成果 JSON').onclick();assert.equal(blob.executions.length,2);assert.equal(blob.exportScope.consistentSnapshot,true);assert.equal(blob.exportScope.snapshot,'old');assert.equal(blob.exportScope.allPagesLoaded,true);
 }else{
  assert.equal(button('导出成果 JSON').disabled,true);button('导出成果 JSON').onclick();assert.equal(blob,undefined);
  assert.equal(descendants(content).filter(e=>e.tagName==='summary'&&e.children.some(c=>c.textContent==='read_file')).length,1);
  await button('重新加载成果记录').onclick();assert.equal(phase,1);assert.equal(button('导出成果 JSON').disabled,false);assert.ok(button('加载更多成果记录'));
  button('导出成果 JSON').onclick();assert.equal(blob.exportScope.snapshot,'new');assert.equal(blob.executions.length,1);assert.equal(blob.exportScope.allPagesLoaded,false);
 }
}
function overviewScenario(){
 const content=new Element('div'),context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 let chosen,older=0;
 const session={number:9,title:'IO fixture',runsTotal:3,hasOlder:true,runs:[{id:'round',prompt:'Create diagram',status:'completed',attachments:[{path:'input.csv',source:'ssh-data'}],steers:[{content:'Use blue'}],steps:[{name:'chat',status:'completed',content:'Diagram response'},{name:'file_application_receipt',content:'receipt'}],files:[{path:'diagram.svg',applied:false}],toolUses:[{tool:'write_file'}]}]};
 context.window.AideTaskOutcome.sessionOverview(content,{session,t:(key,...a)=>key.replace(/\{(\d+)\}/g,(_,i)=>a[i]),selectRun:id=>chosen=id,loadOlder:()=>older++});
 const all=descendants(content),texts=all.map(e=>e.textContent||'').join('\n');
 for(const text of ['输入了什么','输出了什么','形成了什么成果','Create diagram','input.csv','ssh-data','Use blue','Diagram response','diagram.svg','提案未应用','已展示 1 / 3'])assert.ok(texts.includes(text),text);
 assert.ok(!texts.includes('receipt'),'system receipt must not count as a model reply');
 all.find(e=>e.textContent==='查看此轮详情与证据').onclick();assert.equal(chosen,'round');
 all.find(e=>e.textContent==='加载更早的输入与输出').onclick();assert.equal(older,1);
}
(async()=>{overviewScenario();for(const kind of ['stable','409','mismatch'])await scenario(kind);console.log('PASS outcome pagination snapshot, stale export guard and explicit reload');})().catch(e=>{console.error(e);process.exitCode=1;});
