const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element {
 constructor(tag){this.tagName=tag;this.children=[];this.isConnected=true;this.disabled=false;this.open=false;this.textContent='';}
 append(...items){this.children.push(...items);}
 replaceChildren(...items){this.children=items;}
 click(){} setAttribute(){} querySelector(){return null;} scrollIntoView(){}
}
const descendants=root=>[root,...root.children.flatMap(descendants)];
const page=(snapshot,offset=0)=>({snapshot,taskId:'test',goal:'Fixture',mode:'chat',status:'running',outputs:[{name:'plan',status:'completed',content:'Internal stage output'},{name:'chat',status:'completed',content:'Visible final answer'}],summary:{files:2,executions:2},files:[{path:'file'+offset,proposedHash:'hash'}],executions:[{id:'E'+offset,tool:'read_file',digest:'digest',preview:'return'}],journal:{sequence:2,url:'/api/sessions/s/runs/r/journal'},verification:[],gaps:[],release:{message:'not recorded'},page:{filesMore:offset===0,executionsMore:offset===0,fileNext:offset+1,executionNext:offset+1}});
async function releaseSourceScenario(){
 const content=new Element('div');
 const urls=['https://github.com/example/aide/releases/tag/v0.1.17.0-RC2','javascript:alert(1)','https://user:secret@example.com/release','https://example.com/release?token=secret','https://example.com/release#secret','//example.com/release','https:\\example.com/release','https://example.com/<img>'];
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},URL,setTimeout(){},setInterval(){return 1;},clearInterval(){},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const data=page('release');data.status='completed';data.release={message:'operator record, not independently verified',receipts:urls.map(url=>({tag:'v0.1.17.0-RC2',url,observedAt:'2026-10-17T00:00:00Z',deployment:'unknown',commit:'c'.repeat(40),artifacts:[],digest:'d'.repeat(64)}))};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api:async()=>data,t:x=>x,blocked:()=>false});
 const all=descendants(content),links=all.filter(e=>e.tagName==='a');
 assert.equal(links.length,2,'only plain HTTPS sources become links; untrusted markup remains text');
 assert.equal(links[0].textContent,urls[0]);assert.equal(links[0].href,urls[0]);
 for(const link of links){assert.equal(link.target,'_blank');assert.equal(link.rel,'noopener noreferrer');assert.equal(link.referrerPolicy,'no-referrer');}
 for(const url of urls.slice(1,7))assert.ok(all.some(e=>e.tagName==='code'&&e.textContent===url),'unsafe sources remain visible inert text');
 assert.equal(links[1].textContent,urls[7]);assert.equal(all.some(e=>e.tagName==='img'),false);
 assert.ok(all.some(e=>e.textContent===data.release.message),'operator provenance boundary stays visible');
}
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
async function executionJournalScenario(){
 const content=new Element('div'),calls=[];let journalPage=0;
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setTimeout(){},setInterval(){return 1;},clearInterval(){},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const api=async path=>{calls.push(path);if(path.endsWith('/journal?limit=100')){journalPage++;return {events:[{version:1,taskId:'r',sequence:1,kind:'tool_dispatch',step:'chat',callId:'call-1',tool:'run_shell',at:'2026-10-14T00:00:00Z',digest:'dispatch-digest'}],next:1,hasMore:true};}if(path.includes('/journal?limit=100&after=1'))return {events:[{version:1,taskId:'r',sequence:2,kind:'tool_result',step:'chat',callId:'call-1',tool:'run_shell',at:'2026-10-14T00:00:01Z',digest:'result-digest'}],next:2,hasMore:false,warning:'torn tail ignored'};return page('stable');};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api,t:x=>x,blocked:()=>false});
 assert.equal(calls.some(path=>path.includes('/journal')),false,'journal stays lazy until the user opens it');
 let details=descendants(content).find(e=>e.className==='outcome-journal');assert.ok(details&&!details.open);details.open=true;details.ontoggle.call(details);await new Promise(resolve=>setImmediate(resolve));
 assert.equal(journalPage,1);let all=descendants(content);assert.ok(all.some(e=>e.textContent==='工具已派发'));assert.ok(all.some(e=>e.textContent.includes('call-1')));assert.ok(all.some(e=>e.textContent.includes('dispatch-digest')));
 let more=all.find(e=>e.tagName==='button'&&e.textContent==='加载更多执行事件');assert.ok(more);await more.onclick();all=descendants(content);assert.ok(all.some(e=>e.textContent==='工具返回已记录'));assert.ok(all.some(e=>e.textContent==='torn tail ignored'));assert.ok(calls.some(path=>path.includes('after=1')),'journal pagination advances using the last persisted sequence');
}
async function terminalJournalDrainScenario(){
 const content=new Element('div'),timers=[];let outcomeCalls=0,journalCalls=0;
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setTimeout(){},setInterval(fn,ms){timers.push({fn,ms});return timers.length;},clearInterval(){},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const running={...page('running'),status:'running'},complete={...page('complete'),status:'completed'};
 const api=async path=>{if(path.includes('/journal?')){journalCalls++;return journalCalls===1?{events:[{sequence:1,kind:'tool_dispatch',step:'chat',tool:'run_shell',at:'t1'}],next:1,hasMore:true}:{events:[{sequence:2,kind:'tool_result',step:'chat',tool:'run_shell',at:'t2'}],next:2,hasMore:false};}outcomeCalls++;return outcomeCalls===1?running:complete;};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api,t:x=>x,blocked:()=>false});
 let details=descendants(content).find(e=>e.className==='outcome-journal');details.open=true;details.ontoggle.call(details);await new Promise(resolve=>setImmediate(resolve));
 assert.equal(timers.length,1);await timers[0].fn();const all=descendants(content);assert.ok(all.some(e=>e.textContent==='工具返回已记录'),'the final poll drains the last journal event before stopping on a terminal outcome');assert.equal(journalCalls,2);
}
async function journalRetryScenario(){
 const content=new Element('div');let journalCalls=0;
 const context={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},setTimeout(){},setInterval(){return 1;},clearInterval(){},encodeURIComponent,matchMedia:()=>({matches:true})};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),context);
 const api=async path=>{if(path.includes('/journal?')){journalCalls++;if(journalCalls===1)throw new Error('temporary journal error');return {events:[{sequence:1,kind:'model_request',step:'chat',at:'t'}],next:1,hasMore:false};}return page('stable');};
 await context.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api,t:x=>x,blocked:()=>false});
 let details=descendants(content).find(e=>e.className==='outcome-journal');details.open=true;details.ontoggle.call(details);await new Promise(resolve=>setImmediate(resolve));
 let retry=descendants(content).find(e=>e.tagName==='button'&&e.textContent==='重试读取');assert.ok(retry,'initial journal failures offer an explicit retry');await retry.onclick();assert.equal(journalCalls,2);assert.ok(descendants(content).some(e=>e.textContent==='模型请求已记录'));
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
 await mounted.ready;await timers[0].fn();assert.equal(calls,1);assert.equal(updated.runs[0].status,'completed');assert.equal(updated.runs[0].steps[0].content,'The final answer');
 mounted.close();assert.ok(cleared.includes(1),'overview polling stops when the view closes');
}
async function stagedOutcomeScenario(){
 const content=new Element('div'),timers=[];let calls=0,blob;
 const c={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)},Blob:class{constructor(parts){blob=JSON.parse(parts[0]);}},URL:{createObjectURL:()=>'',revokeObjectURL(){}},setTimeout(){},setInterval(fn){timers.push(fn);return timers.length;},clearInterval(){},encodeURIComponent};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),c);
 const one={id:'one',index:1,round:0,content:'STAGE_ONE_已保存',digest:'a'.repeat(64),truncated:false};
 const two={id:'two',index:2,round:1,content:'STAGE_TWO_最终',digest:'b'.repeat(64),truncated:false};
 const first={...page('one'),mode:'chat',outputs:[{name:'chat',status:'running',content:'',segments:[one],segmentsTotal:1,segmentsOmitted:0}],page:{filesMore:false,executionsMore:false}};
 const last={...first,snapshot:'two',status:'completed',outputs:[{name:'chat',status:'completed',content:two.content,digest:two.digest,segments:[one,two],segmentsTotal:2,segmentsOmitted:0}]};
 await c.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api:async()=>++calls===1?first:last,t:x=>x,blocked:()=>false});
 const shown=text=>descendants(content).filter(e=>e.className==='outcome-io-answer md-body'&&e.textContent===text).length;
 assert.equal(shown(one.content),1,'saved conclusion must be visible while next request is still running');
 assert.ok(descendants(content).some(e=>e.textContent==='任务仍在运行，以下是已保存的阶段正文。'));
 await timers[0]();assert.equal(shown(one.content),1);assert.equal(shown(two.content),1,'final body matching a saved segment must not be rendered twice');
 assert.ok(!descendants(content).some(e=>e.textContent==='任务仍在运行，以下是已保存的阶段正文。'||e.textContent==='以下是已保存的阶段正文；任务尚未正常完成。'),'completed replies must not retain a running or incomplete notice');
 descendants(content).find(e=>e.textContent==='导出成果 JSON').onclick();assert.equal(blob.outputs[0].segments.length,2);assert.equal(blob.exportScope.outputTextComplete,true);
 const truncated={...last,outputs:[{...last.outputs[0],segmentsOmitted:1,segmentsTotal:3}]};
 await c.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api:async()=>truncated,t:x=>x,blocked:()=>false});
 descendants(content).find(e=>e.textContent==='导出成果 JSON').onclick();assert.equal(blob.exportScope.allPagesLoaded,true);assert.equal(blob.exportScope.outputTextComplete,false);assert.equal(blob.exportScope.textTruncated,true,'loaded execution pages do not imply all output text is present');
 const collision={...last,outputs:[{name:'chat',status:'completed',content:'shared preview',digest:'b'.repeat(64),truncated:true,segments:[{...one,content:'shared preview',truncated:true}],segmentsTotal:1,segmentsOmitted:0}]};
 await c.window.AideTaskOutcome.mount(content,{sessionId:'s',runId:'r',api:async()=>collision,t:x=>x,blocked:()=>false});
 assert.equal(shown('shared preview'),2,'different whole digests must retain both outputs even when truncated previews are identical');
}
function stagedOverviewScenario(){
 const content=new Element('div'),c={window:{addEventListener(){}},document:{createElement:tag=>new Element(tag)}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/task-outcome.js','utf8'),c);
 const session={id:'s',title:'Saved stages',runsTotal:1,runs:[{id:'r',mode:'chat',status:'paused',steps:[{name:'chat',status:'paused',content:'SAME_TEXT',reasoning:'PRIVATE_REASONING',outputSegments:[{id:'a',round:0,content:'SAME_TEXT'},{id:'b',round:1,content:'SAME_TEXT'}]}]}]};
 c.window.AideTaskOutcome.sessionOverview(content,{session,t:x=>x,selectRun(){},loadOlder(){}});
 let all=descendants(content);assert.equal(all.filter(e=>e.className==='outcome-io-answer md-body'&&e.textContent==='SAME_TEXT').length,2,'distinct saved stages with repeated text remain; only duplicate legacy final is omitted');assert.ok(!all.some(e=>e.textContent==='PRIVATE_REASONING'));
 assert.ok(all.some(e=>e.textContent==='以下是已保存的阶段正文；任务尚未正常完成。'),'paused replies retain an explicit incomplete notice');
 const metrics=all.find(e=>e.className==='outcome-metrics outcome-io-metrics');assert.equal(metrics.children[1].children[0].textContent,'1','stages still represent one assistant turn');
 session.runs[0].steps[0].content='';session.runs[0].steps[0].outputSegments=Array.from({length:120},(_,i)=>({id:String(i),round:i,content:'界'.repeat(9000)}));
 c.window.AideTaskOutcome.sessionOverview(content,{session,t:x=>x,selectRun(){},loadOlder(){}});all=descendants(content);const bodies=all.filter(e=>e.className==='outcome-io-answer md-body');assert.equal(bodies.length,4);assert.equal(bodies.reduce((n,e)=>n+Array.from(e.textContent).length,0),32000,'overview uses a bounded Unicode preview as well as the API');
}
(async()=>{await stagedOutcomeScenario();stagedOverviewScenario();await releaseSourceScenario();overviewScenario();overviewReplyCountScenario();await overviewRefreshScenario();for(const kind of ['stable','409','mismatch'])await scenario(kind);await runningRefreshScenario();await executionJournalScenario();await terminalJournalDrainScenario();await journalRetryScenario();console.log('PASS outcome overview reply counts, live refresh, polling cleanup, pagination snapshot, stale export guard, explicit reload, running refresh, lazy execution journal, terminal journal drain and retry');})().catch(e=>{console.error(e);process.exitCode=1;});
