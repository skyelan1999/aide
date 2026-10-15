/* A task outcome collects records without promoting model claims to proof. */
(() => {
 'use strict';
 let generation=0,active=null;
 const node=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined)e.textContent=String(text);return e;};
 const replyNames=['chat','agent','review','propose','plan'];
 const preferredReply={chat:'chat',workflow:'agent',review:'review',propose:'propose',plan:'plan'};
 const finalOutput=(steps,mode)=>{const available=(steps||[]).filter(s=>s?.content&&s.name!=='file_application_receipt');const preferred=preferredReply[mode];return (preferred&&[...available].reverse().find(s=>s.name===preferred))||[...available].reverse().find(s=>replyNames.includes(s.name))||available[available.length-1]||null;};
 function shellWriteTargets(toolUses){
  const found=[];
  for(const [index,use] of (toolUses||[]).entries()){
   if(use.tool!=='run_shell')continue;
   let command='';try{command=JSON.parse(use.args||'{}').command||'';}catch{}
   let word='',quote='',escaped=false;const tokens=[];
   const flush=()=>{if(word){tokens.push(word);word='';}};
   for(let i=0;i<command.length;i++){
    const c=command[i];
    if(escaped){word+=c;escaped=false;continue;}
    if(quote==="'"){if(c==="'")quote='';else word+=c;continue;}
    if(quote==='"'){if(c==='"')quote='';else if(c==='\\')escaped=true;else word+=c;continue;}
    if(c==='\\'){escaped=true;continue;}
    if(c==='"'||c==="'"){quote=c;continue;}
    if(c==='#'&&!word){while(i+1<command.length&&command[i+1]!=='\n')i++;continue;}
    if(c==='>'){flush();if(command[i+1]==='>'){tokens.push('>>');i++;}else tokens.push('>');continue;}
    if(c===' '||c==='\t'||c==='\r'||c==='\n'||c===';'||c==='|'){flush();continue;}
    if(c==='&'){flush();tokens.push('&');continue;}
    word+=c;
   }
   flush();
   for(let i=0;i+1<tokens.length;i++)if(tokens[i]==='>'||tokens[i]==='>>'){
    const path=tokens[i+1];if(!path||path==='&'||path.startsWith('&'))continue;
    found.push({path,call:index+1,resultRecorded:!!(use.result||use.preview)});
   }
  }
  return found;
 }
 function close(){generation++;active?.close();}
 window.LockCluster?.on('effective',value=>{if(value)close();});
 window.addEventListener('pagehide',close);
 function sessionOverview(content,{session,t,selectRun,loadOlder,renderMarkdown,api,revision,blocked,onSession}){
  close();const gen=++generation;let current=session,pollTimer=null,polling=false,closed=false;
  const valid=()=>!closed&&gen===generation&&content.isConnected&&!blocked?.();
  const stop=()=>{closed=true;if(pollTimer!==null)clearInterval(pollTimer);pollTimer=null;if(active?.close===stop)active=null;};active={close:stop};
  function draw(){session=current;content.replaceChildren();
  const runs=session.runs||[],intro=node('div','outcome-intro');
  intro.append(node('h3','',t('当前会话 · 输入与输出')),node('p','muted','#'+(session.number||'?')+' · '+session.title),node('p','muted',t('已展示 {0} / {1} 轮任务',runs.length,session.runsTotal??runs.length)));content.append(intro);
  const replyFor=run=>finalOutput(run.steps,run.mode);
  const runTargets=new Map(runs.map(run=>[run.id,shellWriteTargets(run.toolUses)]));
  const totals={inputs:runs.length,outputs:runs.filter(r=>replyFor(r)).length,files:runs.reduce((n,r)=>n+(r.files||[]).length,0),writeTargets:[...runTargets.values()].reduce((n,items)=>n+items.length,0)};
  const metrics=node('div','outcome-metrics outcome-io-metrics');for(const [key,label]of [['inputs','输入轮次'],['outputs','助手回复'],['files','文件成果'],['writeTargets','写入目标线索']]){const cell=node('div');cell.append(node('strong','',totals[key]),node('span','',t(label)));metrics.append(cell);}content.append(metrics);
  const text=(host,value,markdown=false)=>{const body=node(markdown?'div':'pre',markdown?'outcome-io-answer md-body':'outcome-io-text');const source=String(value||'');if(markdown&&renderMarkdown)body.innerHTML=renderMarkdown(source);else body.textContent=source;host.append(body);};
  const phaseName=name=>({chat:t('助手回复'),agent:t('自主执行'),review:t('审查'),propose:t('方案'),plan:t('规划')})[name]||name||t('处理阶段');
  for(let i=runs.length-1;i>=0;i--){const run=runs[i],card=node('article','outcome-io-round outcome-io-turn'),number=(session.runsTotal??runs.length)-runs.length+i+1,final=replyFor(run);
   const head=node('header','outcome-io-turn-head');head.append(node('strong','',t('第 {0} 轮',number)),node('time','',run.created||''),node('span','outcome-io-status',t(run.status||'未知状态')));card.append(head);
   const input=node('section','outcome-io-block outcome-io-user');input.append(node('h4','','你'));text(input,run.prompt);
   for(const a of run.attachments||[])input.append(node('p','outcome-io-resource',(a.directory?t('文件夹'):t('附件'))+' · '+a.path+' · '+(a.source||a.root||'workspace')));
   for(const steer of run.steers||[])input.append(node('p','outcome-io-resource',t(steer.queued?'排队补充':'插话补充')+' · '+steer.content));
   const output=node('section','outcome-io-block outcome-io-assistant');output.append(node('h4','','助手'));
   if(final){output.append(node('p','outcome-io-model',(run.model||'Aide')+' · '+phaseName(final.name)));text(output,final.content,true);}
   else if(run.error)output.append(node('p','outcome-io-error','⚠ '+run.error));
   else output.append(node('p','muted',t(run.status==='running'?'正在生成回复…':'没有记录到助手正文')));
   if(final&&run.error)output.append(node('p','outcome-io-error','⚠ '+run.error));
   const artifacts=node('section','outcome-io-block outcome-io-artifacts');artifacts.append(node('h4','','文件成果'));
   const targets=runTargets.get(run.id)||[];
   if(!(run.files||[]).length&&!targets.length)artifacts.append(node('p','muted',t('本轮没有文件变更记录')));
   for(const f of run.files||[])artifacts.append(node('p','outcome-io-resource',f.path+' · '+t(f.applied?'已记录应用，仍需复核':'变更提案，尚无应用标记')));
   for(const target of targets){const hint=node('p','outcome-io-resource',t('命令写入目标线索')+' · '+target.path+' · '+t('工具调用 {0}',target.call)+' · '+t('未验证文件存在或正文'));artifacts.append(hint);}
   const processSteps=(run.steps||[]).filter(s=>s!==final&&s.content&&s.name!=='file_application_receipt'),toolUses=run.toolUses||[];
   if(processSteps.length||toolUses.length){const activity=node('details','outcome-io-activity');activity.append(node('summary','',t('查看运行过程 · {0} 阶段 · {1} 次工具调用',processSteps.length,toolUses.length)));
    for(const step of processSteps){const d=node('details','outcome-io-process-step');d.append(node('summary','',phaseName(step.name)+' · '+t(step.status||'已记录')));text(d,step.content,true);activity.append(d);}
    for(const [index,use]of toolUses.entries()){const d=node('details','outcome-io-tool');d.append(node('summary','',t('工具调用 {0}',index+1)+' · '+(use.tool||t('未知工具'))));if(use.childTitle||use.childNumber)d.append(node('p','muted',t('子会话')+' · '+(use.childTitle||('#'+use.childNumber))));if(use.args)d.append(node('h5','','参数'),node('pre','outcome-io-text',use.args));if(use.preview||use.result)d.append(node('h5','','返回'),node('pre','outcome-io-text',use.preview||use.result));activity.append(d);}
    card.append(activity);
   }
   const detail=node('button','quiet outcome-io-evidence-link',t('查看本轮详情、审批与证据'));detail.type='button';detail.onclick=()=>selectRun(run.id);
   card.append(input,output,artifacts,detail);content.append(card);
  }
  if(session.hasOlder){const more=node('button','quiet',t('加载更早的输入与输出'));more.type='button';more.onclick=loadOlder;content.append(more);}
  content.append(node('p','outcome-boundary',t('这里展示已提交的输入和已记录的输出；未发送草稿不计入。已应用记录不代表当前文件已复核。')));
  }
  draw();
  const needsPolling=value=>(value.runs||[]).some(run=>run?.status==='running')||value.runningRuns>0||(!Number.isInteger(value.runningRuns)&&value.hasOlder);
  if(api&&needsPolling(current)){
   const initialRevision=revision?revision(current):JSON.stringify(current);
   let lastRevision=initialRevision;
   const poll=async()=>{if(!valid()){stop();return;}if(polling)return;polling=true;try{const next=await api();if(!valid())return;const nextRevision=revision?revision(next):JSON.stringify(next);current=next;if(!needsPolling(next))stop();if(nextRevision!==lastRevision){lastRevision=nextRevision;if(onSession)onSession(next);else draw();}}catch{/* Preserve the last readable overview and retry on the next interval. */}finally{polling=false;}};
   pollTimer=setInterval(poll,2500);
  }
  return {close:stop};
 }
 async function mount(content,{sessionId,runId,api,blocked,t,error,renderMarkdown}){
   close();const gen=++generation;let pollTimer=null;const stop=()=>{if(pollTimer!==null)clearInterval(pollTimer);pollTimer=null;if(active?.close===stop)active=null;};active={close:stop};
   content.replaceChildren(node('p','muted',t('正在读取任务记录…')));
   const valid=()=>gen===generation&&content.isConnected&&!blocked?.();
   const base='/sessions/'+encodeURIComponent(sessionId)+'/runs/'+encodeURIComponent(runId)+'/outcome';
  let data,files=[],executions=[],writeTargets=[],stale=false,hasLoadedMore=false,polling=false;
  let journalEvents=[],journalNext=0,journalHasMore=false,journalWarning='',journalLoaded=false,journalLoading=false,journalError='';
   async function reloadRecords(){
    try{const next=await api(base);if(!valid())return;data=next;files=next.files;executions=next.executions;writeTargets=next.writeTargets||[];stale=false;hasLoadedMore=false;render();syncPolling();}
    catch(e){if(valid())error?.(e.message);}
   }
   const section=(title)=>{const s=node('section','outcome-section');s.append(node('h3','',t(title)));content.append(s);return s;};
   async function evidence(record){
    if(!valid())return;
    try{const raw=await api(base+'/evidence/'+encodeURIComponent(record.id)+'?digest='+encodeURIComponent(record.digest));if(!valid())return;
     const details=node('details','outcome-evidence');details.open=true;details.append(node('summary','',record.id+' · '+raw.tool),node('h4','',t(raw.source==='system'?'系统记录来源':'原始参数')),node('pre','',raw.source==='system'?'Aide · '+raw.tool:raw.args||'—'),node('h4','',t('原始返回')),node('pre','',raw.result||t('没有返回记录')),node('code','outcome-digest',raw.digest));
     const previous=content.querySelector('.outcome-evidence');previous?.remove();content.append(details);details.scrollIntoView({block:'nearest',behavior:matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'});
    }catch(e){if(valid())error?.(e.message);}
   }
   function syncPolling(){
    if(data?.status==='running'&&!stale){if(pollTimer===null)pollTimer=setInterval(poll,2500);}
    else if(pollTimer!==null){clearInterval(pollTimer);pollTimer=null;}
   }
   async function poll(){
    if(!valid()||polling||stale)return;polling=true;
    try{const next=await api(base);if(!valid())return;
     if(next.snapshot!==data.snapshot){
      if(hasLoadedMore){stale=true;render();syncPolling();return;}
      data=next;files=next.files||[];executions=next.executions||[];writeTargets=next.writeTargets||[];render();
     } else if(next.status!==data.status){data={...data,status:next.status};render();}
     // A terminal outcome refresh may contain the final tool result too; drain
     // the journal once before polling stops, even if this poll marks it done.
     if(journalLoaded)await loadJournal(false);
     syncPolling();
    }catch{/* Keep the last readable snapshot and retry on the next interval. */}
    finally{polling=false;}
   }
   async function loadJournal(reset=true){
    if(!valid()||journalLoading||!data?.journal?.url)return;
    journalLoading=true;journalError='';
    if(reset){journalEvents=[];journalNext=0;journalHasMore=false;journalWarning='';}
    render();
    try{
     const path=base+'/journal?limit=100'+(reset?'':'&after='+encodeURIComponent(journalNext));
     const page=await api(path);if(!valid())return;
     const seen=new Set(journalEvents.map(event=>event.sequence));
     journalEvents.push(...(page.events||[]).filter(event=>!seen.has(event.sequence)));
     journalNext=Number(page.next)||journalNext;journalHasMore=!!page.hasMore;journalWarning=page.warning||'';journalLoaded=true;
    }catch(e){if(valid())journalError=e.message||t('执行时间线读取失败');}
    finally{journalLoading=false;if(valid())render();}
   }
   function render(){
    const scrollTop=content.scrollTop||0,opened=[...(content.querySelectorAll?.('details')||[])].map((item,index)=>item.open?index:-1).filter(index=>index>=0);
    content.replaceChildren();const intro=node('div','outcome-intro');intro.append(node('p','muted','#'+(data.sessionNumber||'?')+' · '+data.sessionTitle),node('p','',data.goal),node('code','outcome-digest',data.taskId),node('p','muted',t('任务状态')+' · '+data.status+' / '+t('工作区')+' · '+(data.workspaceId||'—')));if(data.goalTruncated)intro.append(node('p','muted',t('目标摘要已截断，原文摘要指纹保留在导出记录中')));content.append(intro);
    const input=section('输入了什么');input.append(node('pre','outcome-io-text',data.goal));
    for(const a of data.inputs?.attachments||[])input.append(node('p','outcome-io-resource',a.path+' · '+(a.source||a.root||'workspace')));
    for(const steer of data.inputs?.steers||[])input.append(node('p','',t(steer.queued?'排队补充':'插话补充')+' · '+steer.content));
    const output=section('输出了什么'),replies=data.outputs||[];if(!replies.length)output.append(node('p','muted',t('尚无回复记录')));
    const finalReply=finalOutput(replies,data.mode);
    if(finalReply){const answer=node('article','outcome-io-final');answer.append(node('p','outcome-io-model',t('助手回复')+' · '+t(finalReply.status||'已记录')));const body=node('div','outcome-io-answer md-body');if(renderMarkdown)body.innerHTML=renderMarkdown(finalReply.content);else body.textContent=finalReply.content;answer.append(body);if(finalReply.truncated)answer.append(node('p','muted',t('回复摘要已截断，完整回复见会话历史')));output.append(answer);}
    const otherReplies=replies.filter(reply=>reply!==finalReply);if(otherReplies.length){const other=node('details','outcome-io-reply');other.append(node('summary','',t('展开其他阶段输出 · {0}',otherReplies.length)));for(const reply of otherReplies){const phase=node('details','outcome-io-process-step');phase.append(node('summary','',reply.name+' · '+t(reply.status||'已记录')),node('pre','outcome-io-text',reply.content));if(reply.truncated)phase.append(node('p','muted',t('回复摘要已截断，完整回复见会话历史')));other.append(phase);}output.append(other);}
    section('形成了什么成果');
    const metrics=node('div','outcome-metrics');for(const [key,label]of [['files','文件提案'],['writeTargets','写入目标线索'],['applicationRecords','应用记录'],['executions','执行返回'],['verificationReports','验证报告'],['unknownOutcomes','未知结果']]){const cell=node('div');cell.append(node('strong','',data.summary[key]??0),node('span','',t(label)));metrics.append(cell);}content.append(metrics);
    content.append(node('p','outcome-boundary',t('任务状态、模型计划与验证报告不是独立验收证明；展开证据查看原始返回。')));
    if(data.plan?.items?.length){const s=section('目标与计划');for(const item of data.plan.items)s.append(node('p','',item.step+' · '+item.status+(item.evidence?.length?' · '+item.evidence.map(i=>'E'+String(i).padStart(4,'0')).join(', '):'')));}
    if(data.journal?.url){
     const timeline=section('执行时间线');
     const details=node('details','outcome-journal');details.open=journalLoaded;
     const summary=node('summary','',t('持久化事件 · {0}',journalLoaded?journalEvents.length:(data.journal.sequence||0)));
     details.append(summary);
     details.ontoggle=()=>{if(details.open&&!journalLoaded)loadJournal(true);};
     if(!journalLoaded)details.append(node('p','muted',t('按需读取独立执行日志；事件记录时间、阶段和摘要指纹，不包含工具正文。')));
     if(journalLoading)details.append(node('p','muted',t('正在读取执行时间线…')));
     if(journalError){const message=node('p','outcome-io-error',journalError);message.setAttribute('role','status');details.append(message);}
     if(journalWarning){const warning=node('p','outcome-boundary',journalWarning);warning.setAttribute('role','status');details.append(warning);}
     const labels={model_request:'模型请求已记录',model_retry:'模型重试已记录',model_continuation:'模型续接已记录',tool_dispatch:'工具已派发',tool_result:'工具返回已记录'};
     for(const event of journalEvents){
      const item=node('article','outcome-journal-event');
      const heading=node('div','outcome-journal-head');heading.append(node('time','',event.at||''),node('span','outcome-journal-kind',t(labels[event.kind]||event.kind||'未知事件')),node('code','',String(event.sequence??'')));item.append(heading);
      item.append(node('p','',t('阶段')+' · '+(event.step||'—')+(event.tool?' · '+event.tool:'')));
      if(event.callId)item.append(node('p','muted',t('调用编号')+' · '+event.callId));
      if(event.digest)item.append(node('code','outcome-digest',t('摘要指纹')+' · '+event.digest));
      details.append(item);
     }
     if(journalLoaded&&!journalEvents.length&&!journalWarning)details.append(node('p','muted',t('尚无持久化执行事件')));
     if(journalLoaded&&journalHasMore){const more=node('button','quiet',t('加载更多执行事件'));more.type='button';more.onclick=()=>loadJournal(false);more.disabled=journalLoading;details.append(more);}
     if(journalError){const retry=node('button','quiet',t('重试读取'));retry.type='button';retry.onclick=()=>loadJournal(!journalEvents.length);details.append(retry);}
     timeline.append(details);
    }
    const fs=section('文件变更');if(!files.length)fs.append(node('p','muted',t('没有文件提案记录')));for(const f of files){const row=node('div','outcome-file');row.append(node('code','',f.path),node('span','muted',t(f.state==='proposed'?'提案未应用':'应用已有记录，当前文件未复核')),node('small','outcome-digest',(f.beforeHash||'—')+' → '+f.proposedHash),node('small','muted',f.proposedBytes+' bytes'));fs.append(row);}
    if(writeTargets.length){const hints=section('命令写入目标线索');hints.append(node('p','muted',data.writeTargetNote||t('命令目标线索不证明写入或当前文件内容')));for(const target of writeTargets){const row=node('div','outcome-file');row.append(node('code','',target.path),node('span','muted',target.execution+' · '+t('命令重定向目标，未回读验证')),node('small','outcome-digest',target.digest||'—'));const execution=executions.find(item=>item.id===target.execution);if(execution){const inspect=node('button','quiet',t('查看原始证据'));inspect.type='button';inspect.onclick=()=>evidence(execution);row.append(inspect);}hints.append(row);}}
    const es=section('执行与证据');if(!executions.length)es.append(node('p','muted',t('没有工具返回记录')));for(const record of executions){const row=node('details','outcome-execution'),summary=node('summary');summary.append(node('code','',record.id),node('span','',record.tool),node('small','muted',t(record.state==='no_result'?'没有返回记录':'已记录返回')));row.append(summary,node('pre','',record.preview||'—'));if(record.truncated)row.append(node('p','muted',t('预览已截断，可展开原始证据')));const b=node('button','quiet',t('查看原始证据'));b.onclick=()=>evidence(record);row.append(b,node('small','outcome-digest',record.digest));es.append(row);}
    if(stale){const notice=node('p','outcome-boundary',t('成果记录已更新，请重新加载；旧页与新页不能混合'));notice.setAttribute('role','status');const reload=node('button','quiet',t('重新加载成果记录'));reload.onclick=async()=>{reload.disabled=true;await reloadRecords();if(valid()&&reload.isConnected)reload.disabled=false;};es.append(notice,reload);}else if(data.page.filesMore||data.page.executionsMore){const more=node('button','quiet',t('加载更多成果记录'));more.onclick=async()=>{more.disabled=true;try{const next=await api(base+'?fileOffset='+data.page.fileNext+'&executionOffset='+data.page.executionNext+'&snapshot='+encodeURIComponent(data.snapshot||''));if(!valid())return;if(!data.snapshot||next.snapshot!==data.snapshot){stale=true;render();syncPolling();return;}files.push(...next.files);executions.push(...next.executions);writeTargets.push(...(next.writeTargets||[]));data={...next,files,executions,writeTargets};hasLoadedMore=true;render();syncPolling();}catch(e){if(valid()){if(e.status===409){stale=true;render();syncPolling();}else{error?.(e.message);more.disabled=false;}}}};es.append(more);}
    const receipts=data.systemReceipts||[];
    if(receipts.length){const rs=section('系统应用回执');rs.append(node('p','muted',data.systemReceiptNote||t('历史系统记录，不代表当前文件或完整验收。')));for(const receipt of receipts){const d=node('details','outcome-execution');d.append(node('summary','',receipt.id+' · '+t(receipt.state==='failed'?'回执检查失败':'系统记录')),node('pre','',receipt.content||'—'));const b=node('button','quiet',t('查看原始证据'));b.onclick=()=>evidence(receipt);d.append(b,node('small','outcome-digest',receipt.digest));if(receipt.truncated)d.append(node('p','muted',t('预览已截断，可展开原始证据')));rs.append(d);}}
    const approvals=section('审批依据');approvals.append(node('p','muted',data.approvalNote||t('历史记录可能缺少审批来源')));for(const review of data.approvals||[]){const entry=node('details'),source={model:'模型独立审核',human:'人工确认',remembered:'授权规则匹配'};entry.append(node('summary','',t(source[review.source]||'历史来源未记录')+' · '+t(({approved:'已自动放行',confirmed:'人工已确认',declined:'人工未批准',reviewing:'审核中',manual:'需手动确认',aborted:'未自动放行'})[review.status]||'历史状态未记录')),node('p','',review.reason),node('pre','',review.command),node('p','muted',(review.at||'')+' · '+(review.workspace||'—')+' · '+(review.root?.displayHost||review.root?.containerAbs||'—')),node('code','outcome-digest',review.fingerprint||'—'));if(review.ruleId)entry.append(node('p','muted',review.ruleId+' · '+(review.expiresAt||t('永久'))));approvals.append(entry);}
    const vs=section('验证记录');if(!data.verification.length)vs.append(node('p','muted',t('尚无验证报告；不推断通过')));for(const report of data.verification){const d=node('details');d.append(node('summary','',report.title+' · '+report.evidence+' · '+t('报告声明')),node('pre','',report.content));if(report.truncated)d.append(node('p','muted',t('报告摘要已截断，原文见对应执行证据')));vs.append(d);}
    if(data.findings?.length){const rs=section('来源与发现');for(const f of data.findings){const d=node('details');d.append(node('summary','',f.name+' · '+f.value+(f.unit?' '+f.unit:'')),node('p','muted',f.kind+' · '+(f.evidence?'E'+String(f.evidence).padStart(4,'0'):t('无来源编号'))),node('blockquote','',f.quote||'—'),node('p','muted',f.limitation||t('可追溯引用不证明来源结论准确')));rs.append(d);}}
    if(data.suggestedCommands?.length){const s=section('建议命令（尚未运行）');for(const command of data.suggestedCommands)s.append(node('pre','',command));}
    if(data.unknownCalls?.length){const s=section('未取得结果的调用');for(const call of data.unknownCalls)s.append(node('p','',call.tool+' · '+(call.callId||'—')+' · '+call.state));}
    if(data.operatorChecks?.length){const s=section('人工核查记录');s.append(node('p','muted',t('操作者观察不替代执行回执；不会重放调用或恢复依赖。')));for(const check of data.operatorChecks){const labels={effect_observed:'观察到效果',no_effect_observed:'未观察到效果',inconclusive:'仍无法判断'};s.append(node('p','',check.callId+' · '+t(Object.hasOwn(labels,check.conclusion)?labels[check.conclusion]:'未知状态')+' · '+check.recordedAt),node('small','outcome-digest',check.digest));}}
    if(data.recoveryContinuation){const c=data.recoveryContinuation,s=section('后续工作继续决策');s.append(node('p','muted',t('不会自动重放旧调用或补跑旧依赖。后续新操作仍须遵循当前审批、沙箱与任务 Hook。')),node('p','',c.sourceRun+' · '+c.recordedAt),node('small','outcome-digest',c.digest));}
    const gaps=section('缺口与发布');for(const gap of data.gaps)gaps.append(node('p','',gap));gaps.append(node('p','muted',data.release.message));
    for(const receipt of data.release.receipts||[]){const card=node('details','outcome-execution');card.append(node('summary','',receipt.tag+' · '+t('操作者记录')),node('p','muted',receipt.observedAt+' · '+receipt.deployment),node('code','outcome-digest',receipt.commit),node('pre','',JSON.stringify(receipt.artifacts,null,2)),node('small','outcome-digest',receipt.digest));gaps.append(card);}
    const receiptFile=node('input');receiptFile.type='file';receiptFile.accept='.json,application/json';receiptFile.hidden=true;
    const importReceipt=node('button','quiet',t('导入发布记录'));importReceipt.type='button';importReceipt.disabled=stale;
    importReceipt.onclick=()=>{if(valid()&&!stale){receiptFile.value='';receiptFile.click();}};
    receiptFile.onchange=async()=>{const file=receiptFile.files?.[0];if(!file||!valid()||stale)return;try{if(file.size>65536)throw new Error(t('发布记录超过64 KiB'));const receipt=JSON.parse(await file.text());if(!valid()||stale)return;
      const preview=node('div','outcome-boundary');preview.append(node('p','',t('此记录由操作者提供，导入不代表远端发布或生产部署已核验。')),node('pre','',JSON.stringify(receipt,null,2)));
      const confirm=node('button','quiet',t('确认导入发布记录')),cancel=node('button','quiet',t('取消'));confirm.type=cancel.type='button';cancel.onclick=()=>preview.remove();
      confirm.onclick=async()=>{if(!valid()||stale||!preview.isConnected)return;confirm.disabled=true;try{await api(base+'/releases',{method:'POST',body:JSON.stringify({snapshot:data.snapshot,receipt})});if(valid())await reloadRecords();}catch(e){if(valid()){error?.(e.message);confirm.disabled=false;}}};preview.append(confirm,cancel);gaps.append(preview);
     }catch(e){if(valid())error?.(e.message);}};
    gaps.append(importReceipt,receiptFile);

    const actions=node('div','outcome-actions');const exportButton=node('button','quiet',t('导出成果 JSON'));exportButton.disabled=stale;exportButton.onclick=()=>{if(!valid()||stale)return;const blob=new Blob([JSON.stringify({...data,files,executions,writeTargets,exportScope:{kind:'summary',snapshot:data.snapshot||null,consistentSnapshot:!!data.snapshot&&!stale,allPagesLoaded:!data.page.filesMore&&!data.page.executionsMore,rawEvidenceIncluded:false,textTruncated:!!data.goalTruncated||executions.some(e=>e.truncated)||data.verification.some(r=>r.truncated)||(data.systemReceipts||[]).some(r=>r.truncated)}},null,2)],{type:'application/json'}),url=URL.createObjectURL(blob),a=node('a');a.href=url;a.download='aide-outcome-'+runId+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};actions.append(exportButton);content.append(actions);
    [...(content.querySelectorAll?.('details')||[])].forEach((item,index)=>{if(opened.includes(index))item.open=true;});content.scrollTop=scrollTop;
   }
   try{data=await api(base);if(!valid())return;files=data.files;executions=data.executions;writeTargets=data.writeTargets||[];render();syncPolling();}catch(e){if(valid()){content.replaceChildren(node('p','task-error',e.message));}}
   return {close};
 }
 window.AideTaskOutcome={mount,sessionOverview,close};
})();
