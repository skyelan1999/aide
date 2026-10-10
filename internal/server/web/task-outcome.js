/* A task outcome collects records without promoting model claims to proof. */
(() => {
 'use strict';
 let generation=0,active=null;
 const node=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined)e.textContent=String(text);return e;};
 function close(){generation++;active?.close();}
 window.LockCluster?.on('effective',value=>{if(value)close();});
 window.addEventListener('pagehide',close);
 function sessionOverview(content,{session,t,selectRun,loadOlder}){
  close();content.replaceChildren();
  const runs=session.runs||[],intro=node('div','outcome-intro');
  intro.append(node('h3','',t('当前会话 · 输入与输出')),node('p','muted','#'+(session.number||'?')+' · '+session.title),node('p','muted',t('已展示 {0} / {1} 轮任务',runs.length,session.runsTotal??runs.length)));content.append(intro);
  const totals={inputs:runs.length,outputs:runs.reduce((n,r)=>n+(r.steps||[]).filter(s=>s.content&&s.name!=='file_application_receipt').length,0),files:runs.reduce((n,r)=>n+(r.files||[]).length,0)};
  const metrics=node('div','outcome-metrics outcome-io-metrics');for(const [key,label]of [['inputs','输入任务'],['outputs','输出回复'],['files','成果文件记录']]){const cell=node('div');cell.append(node('strong','',totals[key]),node('span','',t(label)));metrics.append(cell);}content.append(metrics);
  const text=(host,value)=>{const pre=node('pre','outcome-io-text',String(value||''));host.append(pre);};
  for(let i=runs.length-1;i>=0;i--){const run=runs[i],card=node('article','outcome-io-round');card.append(node('h3','',t('第 {0} 轮', (session.runsTotal??runs.length)-runs.length+i+1)+' · '+(run.created||'')),node('p','muted',t('任务状态')+' · '+run.status));
   const input=node('section','outcome-io-block');input.append(node('h4','',t('输入了什么')));text(input,run.prompt);
   for(const a of run.attachments||[])input.append(node('p','outcome-io-resource',(a.directory?t('文件夹'):t('文件'))+' · '+a.path+' · '+(a.source||a.root||'workspace')));
   for(const steer of run.steers||[])input.append(node('p','',t(steer.queued?'排队补充':'插话补充')+' · '+steer.content));
   const output=node('section','outcome-io-block');output.append(node('h4','',t('输出了什么')));
   const replies=(run.steps||[]).filter(s=>s.content&&s.name!=='file_application_receipt');
   if(!replies.length)output.append(node('p','muted',t('尚无回复记录')));
   for(const step of replies){const d=node('details','outcome-io-reply');d.append(node('summary','',step.name+' · '+step.status));text(d,step.content);output.append(d);}
   const results=node('section','outcome-io-block');results.append(node('h4','',t('形成了什么成果')));
   if(!(run.files||[]).length)results.append(node('p','muted',t('没有成果文件记录；回复和执行结果见输出及详情。')));
   for(const f of run.files||[])results.append(node('p','outcome-io-resource',f.path+' · '+t(f.applied?'已应用记录':'提案未应用')));
   results.append(node('p','muted',t('{0} 条工具调用记录',run.toolUses?.length||0)));
   const detail=node('button','quiet',t('查看此轮详情与证据'));detail.type='button';detail.onclick=()=>selectRun(run.id);results.append(detail);
   card.append(input,output,results);content.append(card);
  }
  if(session.hasOlder){const more=node('button','quiet',t('加载更早的输入与输出'));more.type='button';more.onclick=loadOlder;content.append(more);}
  content.append(node('p','outcome-boundary',t('这里展示已提交的输入和已记录的输出；未发送草稿不计入。已应用记录不代表当前文件已复核。')));
 }
 async function mount(content,{sessionId,runId,api,blocked,t,error}){
   close();const gen=++generation;active={close:()=>{active=null;}};
   content.replaceChildren(node('p','muted',t('正在读取任务记录…')));
   const valid=()=>gen===generation&&content.isConnected&&!blocked?.();
   const base='/sessions/'+encodeURIComponent(sessionId)+'/runs/'+encodeURIComponent(runId)+'/outcome';
   let data,files=[],executions=[],stale=false;
   async function reloadRecords(){
    try{const next=await api(base);if(!valid())return;data=next;files=next.files;executions=next.executions;stale=false;render();}
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
   function render(){
    content.replaceChildren();const intro=node('div','outcome-intro');intro.append(node('p','muted','#'+(data.sessionNumber||'?')+' · '+data.sessionTitle),node('p','',data.goal),node('code','outcome-digest',data.taskId),node('p','muted',t('任务状态')+' · '+data.status+' / '+t('工作区')+' · '+(data.workspaceId||'—')));if(data.goalTruncated)intro.append(node('p','muted',t('目标摘要已截断，原文摘要指纹保留在导出记录中')));content.append(intro);
    const input=section('输入了什么');input.append(node('pre','outcome-io-text',data.goal));
    for(const a of data.inputs?.attachments||[])input.append(node('p','outcome-io-resource',a.path+' · '+(a.source||a.root||'workspace')));
    for(const steer of data.inputs?.steers||[])input.append(node('p','',t(steer.queued?'排队补充':'插话补充')+' · '+steer.content));
    const output=section('输出了什么');if(!data.outputs?.length)output.append(node('p','muted',t('尚无回复记录')));
    for(const reply of data.outputs||[]){const d=node('details','outcome-io-reply');d.append(node('summary','',reply.name+' · '+reply.status),node('pre','outcome-io-text',reply.content));if(reply.truncated)d.append(node('p','muted',t('回复摘要已截断，完整回复见会话历史')));output.append(d);}
    section('形成了什么成果');
    const metrics=node('div','outcome-metrics');for(const [key,label]of [['files','文件提案'],['applicationRecords','应用记录'],['executions','执行返回'],['verificationReports','验证报告'],['unknownOutcomes','未知结果']]){const cell=node('div');cell.append(node('strong','',data.summary[key]),node('span','',t(label)));metrics.append(cell);}content.append(metrics);
    content.append(node('p','outcome-boundary',t('任务状态、模型计划与验证报告不是独立验收证明；展开证据查看原始返回。')));
    if(data.plan?.items?.length){const s=section('目标与计划');for(const item of data.plan.items)s.append(node('p','',item.step+' · '+item.status+(item.evidence?.length?' · '+item.evidence.map(i=>'E'+String(i).padStart(4,'0')).join(', '):'')));}
    const fs=section('文件变更');if(!files.length)fs.append(node('p','muted',t('没有文件提案记录')));for(const f of files){const row=node('div','outcome-file');row.append(node('code','',f.path),node('span','muted',t(f.state==='proposed'?'提案未应用':'应用已有记录，当前文件未复核')),node('small','outcome-digest',(f.beforeHash||'—')+' → '+f.proposedHash),node('small','muted',f.proposedBytes+' bytes'));fs.append(row);}
    const es=section('执行与证据');if(!executions.length)es.append(node('p','muted',t('没有工具返回记录')));for(const record of executions){const row=node('details','outcome-execution'),summary=node('summary');summary.append(node('code','',record.id),node('span','',record.tool),node('small','muted',t(record.state==='no_result'?'没有返回记录':'已记录返回')));row.append(summary,node('pre','',record.preview||'—'));if(record.truncated)row.append(node('p','muted',t('预览已截断，可展开原始证据')));const b=node('button','quiet',t('查看原始证据'));b.onclick=()=>evidence(record);row.append(b,node('small','outcome-digest',record.digest));es.append(row);}
    if(stale){const notice=node('p','outcome-boundary',t('成果记录已更新，请重新加载；旧页与新页不能混合'));notice.setAttribute('role','status');const reload=node('button','quiet',t('重新加载成果记录'));reload.onclick=async()=>{reload.disabled=true;await reloadRecords();if(valid()&&reload.isConnected)reload.disabled=false;};es.append(notice,reload);}else if(data.page.filesMore||data.page.executionsMore){const more=node('button','quiet',t('加载更多成果记录'));more.onclick=async()=>{more.disabled=true;try{const next=await api(base+'?fileOffset='+data.page.fileNext+'&executionOffset='+data.page.executionNext+'&snapshot='+encodeURIComponent(data.snapshot||''));if(!valid())return;if(!data.snapshot||next.snapshot!==data.snapshot){stale=true;render();return;}files.push(...next.files);executions.push(...next.executions);data={...next,files,executions};render();}catch(e){if(valid()){if(e.status===409){stale=true;render();}else{error?.(e.message);more.disabled=false;}}}};es.append(more);}
    const receipts=data.systemReceipts||[];
    if(receipts.length){const rs=section('系统应用回执');rs.append(node('p','muted',data.systemReceiptNote||t('历史系统记录，不代表当前文件或完整验收。')));for(const receipt of receipts){const d=node('details','outcome-execution');d.append(node('summary','',receipt.id+' · '+t(receipt.state==='failed'?'回执检查失败':'系统记录')),node('pre','',receipt.content||'—'));const b=node('button','quiet',t('查看原始证据'));b.onclick=()=>evidence(receipt);d.append(b,node('small','outcome-digest',receipt.digest));if(receipt.truncated)d.append(node('p','muted',t('预览已截断，可展开原始证据')));rs.append(d);}}
    const approvals=section('审批依据');approvals.append(node('p','muted',data.approvalNote||t('历史记录可能缺少审批来源')));for(const review of data.approvals||[]){const entry=node('details'),source={model:'模型独立审核',human:'人工确认',remembered:'授权规则匹配'};entry.append(node('summary','',t(source[review.source]||'历史来源未记录')+' · '+t(({approved:'已自动放行',confirmed:'人工已确认',declined:'人工未批准',reviewing:'审核中',manual:'需手动确认',aborted:'未自动放行'})[review.status]||'历史状态未记录')),node('p','',review.reason),node('pre','',review.command),node('p','muted',(review.at||'')+' · '+(review.workspace||'—')+' · '+(review.root?.displayHost||review.root?.containerAbs||'—')),node('code','outcome-digest',review.fingerprint||'—'));if(review.ruleId)entry.append(node('p','muted',review.ruleId+' · '+(review.expiresAt||t('永久'))));approvals.append(entry);}
    const vs=section('验证记录');if(!data.verification.length)vs.append(node('p','muted',t('尚无验证报告；不推断通过')));for(const report of data.verification){const d=node('details');d.append(node('summary','',report.title+' · '+report.evidence+' · '+t('报告声明')),node('pre','',report.content));if(report.truncated)d.append(node('p','muted',t('报告摘要已截断，原文见对应执行证据')));vs.append(d);}
    if(data.findings?.length){const rs=section('来源与发现');for(const f of data.findings){const d=node('details');d.append(node('summary','',f.name+' · '+f.value+(f.unit?' '+f.unit:'')),node('p','muted',f.kind+' · '+(f.evidence?'E'+String(f.evidence).padStart(4,'0'):t('无来源编号'))),node('blockquote','',f.quote||'—'),node('p','muted',f.limitation||t('可追溯引用不证明来源结论准确')));rs.append(d);}}
    if(data.suggestedCommands?.length){const s=section('建议命令（尚未运行）');for(const command of data.suggestedCommands)s.append(node('pre','',command));}
    if(data.unknownCalls?.length){const s=section('未取得结果的调用');for(const call of data.unknownCalls)s.append(node('p','',call.tool+' · '+(call.callId||'—')+' · '+call.state));}
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

    const actions=node('div','outcome-actions');const exportButton=node('button','quiet',t('导出成果 JSON'));exportButton.disabled=stale;exportButton.onclick=()=>{if(!valid()||stale)return;const blob=new Blob([JSON.stringify({...data,files,executions,exportScope:{kind:'summary',snapshot:data.snapshot||null,consistentSnapshot:!!data.snapshot&&!stale,allPagesLoaded:!data.page.filesMore&&!data.page.executionsMore,rawEvidenceIncluded:false,textTruncated:!!data.goalTruncated||executions.some(e=>e.truncated)||data.verification.some(r=>r.truncated)||(data.systemReceipts||[]).some(r=>r.truncated)}},null,2)],{type:'application/json'}),url=URL.createObjectURL(blob),a=node('a');a.href=url;a.download='aide-outcome-'+runId+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};actions.append(exportButton);content.append(actions);
   }
   try{data=await api(base);if(!valid())return;files=data.files;executions=data.executions;render();}catch(e){if(valid()){content.replaceChildren(node('p','task-error',e.message));}}
   return {close};
 }
 window.AideTaskOutcome={mount,sessionOverview,close};
})();
