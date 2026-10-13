const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element {
 constructor(tag){this.tagName=tag;this.children=[];this.isConnected=true;this.disabled=false;this.open=false;this.textContent='';}
 append(...items){this.children.push(...items);}
 replaceChildren(...items){this.children=items;}
 click(){} setAttribute(){} querySelector(){return null;} scrollIntoView(){}
}
const descendants=root=>[root,...root.children.flatMap(descendants)];
const page=(snapshot,offset=0)=>({snapshot,taskId:'test',goal:'Fixture',mode:'chat',status:'running',outputs:[{name:'plan',status:'completed',content:'Internal stage output'},{name:'chat',status:'completed',content:'Visible final answer'}],summary:{files:2,executions:2},files:[{path:'file'+offset,proposedHash:'hash'}],executions:[{id:'E'+offset,tool:'read_file',digest:'digest',preview:'return'}],verification:[],gaps:[],release:{message:'not recorded'},page:{filesMore:offset===0,executionsMore:offset===0,fileNext:offset+1,executionNext:offset+1}});
async function scenario(kind){
 let calls=[],blob,phase=0;
 const content=new Element('div');
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},Blob:class{constructor(parts){blob=JSON.parse(parts[0]);}},URL:{createObjectURL:()=>'',revokeObjectURL(){}},setTimeout(){},setInterval(){return 1;},clearInterval(){},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const api=async path=>{calls.push(path);if(calls.length===1)return page('old');if(path.includes('snapshot=')){assert.match(path,/snapshot=old/);if(kind==='409'){const e=new Error('changed');e.status=409;throw e;}if(kind==='mismatch')return page('new',1);return page('old',1);}phase++;return page('new');};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api,t:x=>x,blocked:()=>false,error:e=>{throw new Error(e);}});
 const initial=descendants(content),final=initial.find(e=>e.className==='outcome-io-final');assert.ok(final&&descendants(final).some(e=>e.textContent.includes('Visible final answer')),'the final assistant answer must be visible by default');assert.ok(initial.some(e=>e.tagName==='details'&&e.className==='outcome-io-reply'&&!e.open),'other stage outputs stay collapsed');
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
async function runningRefreshScenario(){
 const content=new Element('div'),timers=[],cleared=[];let calls=0;
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setTimeout(){},setInterval(fn,ms){timers.push({fn,ms});return timers.length;},clearInterval(id){cleared.push(id);},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const running={...page('first'),status:'running',outputs:[{name:'chat',status:'running',content:'首段生成中'}]};
 const complete={...page('second'),status:'completed',outputs:[{name:'chat',status:'completed',content:'完整回复已生成'}]};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api:async()=>++calls===1?running:complete,t:x=>x,blocked:()=>false});
 assert.equal(timers.length,1);assert.equal(timers[0].ms,2500);
 await timers[0].fn();
 assert.ok(descendants(content).some(e=>e.textContent==='完整回复已生成'),'running outcome refreshes the first page as the model produces output');
 assert.ok(cleared.includes(1),'polling stops once the run is terminal');
}
function overviewScenario(){
 const content=new Element('div'),context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 let chosen,older=0;
 const session={number:9,title:'IO fixture',runsTotal:3,hasOlder:true,runs:[{id:'round',mode:'review',prompt:'Review diagram',status:'completed',attachments:[{path:'input.csv',source:'ssh-data'}],steers:[{content:'Use blue'}],steps:[{name:'chat',status:'completed',content:'Intermediate chat response'},{name:'review',status:'completed',content:'Review findings'},{name:'file_application_receipt',content:'receipt'}],files:[{path:'diagram.svg',applied:false}],toolUses:[{tool:'run_shell',args:JSON.stringify({command:'printf x # > comment-only.txt\nprintf y > actual.txt'}),result:'exit code: 0'},{tool:'write_file'}]}]};
 context.window.AideTaskOutcome.sessionOverview(content,{session,t:(key,...a)=>key.replace(/\{(\d+)\}/g,(_,i)=>a[i]),selectRun:id=>chosen=id,loadOlder:()=>older++});
 const all=descendants(content),texts=all.map(e=>e.textContent||'').join('\n');
 for(const text of ['你','助手','文件成果','Review diagram','input.csv','ssh-data','Use blue','Intermediate chat response','Review findings','diagram.svg','变更提案，尚无应用标记','已展示 1 / 3','actual.txt'])assert.ok(texts.includes(text),text);
 assert.ok(!all.some(e=>String(e.className||'').includes('outcome-io-resource')&&e.textContent.includes('comment-only.txt')),'comment text must not be reported as a shell write target');
 const assistant=all.find(e=>String(e.className||'').includes('outcome-io-assistant')),answer=assistant&&descendants(assistant).find(e=>String(e.className||'').includes('outcome-io-answer'));assert.ok(answer&&answer.textContent==='Review findings','review-mode summary must show the review output as final');assert.ok(!answer.textContent.includes('Intermediate chat response'),'intermediate chat output must not replace the mode-specific final output');
 assert.ok(!texts.includes('receipt'),'system receipt must not count as a model reply');
 all.find(e=>e.textContent==='查看本轮详情、审批与证据').onclick();assert.equal(chosen,'round');
 all.find(e=>e.textContent==='加载更早的输入与输出').onclick();assert.equal(older,1);
}
function overviewReplyCountScenario(){
 const content=new Element('div'),context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const session={number:10,title:'Reply count fixture',runsTotal:2,runs:[
  {id:'failed',mode:'chat',prompt:'Request that fails',status:'failed',error:'provider unavailable',steps:[],files:[],toolUses:[]},
  {id:'answered',mode:'chat',prompt:'Request answered',status:'completed',steps:[{name:'chat',status:'completed',content:'Actual answer'}],files:[],toolUses:[]}
 ]};
 context.window.AideTaskOutcome.sessionOverview(content,{session,t:x=>x,selectRun(){},loadOlder(){}});
 const metrics=descendants(content).find(e=>String(e.className||'').includes('outcome-io-metrics'));
 const values=metrics.children.map(cell=>cell.children[0]?.textContent);
 assert.deepEqual(values,['2','1','0','0'],'failed rounds must not be counted as assistant replies');
 const failed=descendants(content).find(e=>String(e.className||'').includes('outcome-io-error'));
 assert.ok(failed&&failed.textContent.includes('provider unavailable'),'the failure remains visible on its own turn');
}
async function overviewRefreshScenario(){
 const content=new Element('div'),timers=[],cleared=[];let updated,calls=0;
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setInterval(fn,ms){timers.push({fn,ms});return timers.length;},clearInterval(id){cleared.push(id);}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const session={id:'s',number:9,title:'live overview',runsTotal:1,runs:[{id:'r',mode:'chat',prompt:'Question',status:'running',steps:[{name:'chat',status:'running',content:''}]}]};
 const complete={...session,updated:'later',runs:[{...session.runs[0],status:'completed',steps:[{name:'chat',status:'completed',content:'The final answer'}]}]};
 const mounted=context.window.AideTaskOutcome.sessionOverview(content,{session,t:x=>x,selectRun(){},loadOlder(){},api:async()=>{calls++;return complete;},revision:s=>s.updated||'initial',blocked:()=>false,onSession:s=>{updated=s;}});
 assert.equal(timers.length,1);assert.equal(timers[0].ms,2500);assert.ok(descendants(content).some(e=>e.textContent==='正在生成回复…'));
 await timers[0].fn();assert.equal(calls,1);assert.equal(updated.runs[0].status,'completed');assert.equal(updated.runs[0].steps[0].content,'The final answer');
 mounted.close();assert.ok(cleared.includes(1),'overview polling stops when the view closes');
}
(async()=>{overviewScenario();overviewReplyCountScenario();await overviewRefreshScenario();for(const kind of ['stable','409','mismatch'])await scenario(kind);await runningRefreshScenario();console.log('PASS outcome overview reply counts, live refresh, polling cleanup, pagination snapshot, stale export guard, explicit reload and running refresh');})().catch(e=>{console.error(e);process.exitCode=1;});
