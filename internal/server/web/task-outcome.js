/* A task outcome collects records without promoting model claims to proof. */
(() => {
 'use strict';
 let generation=0,active=null;
 const node=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined)e.textContent=String(text);return e;};
 function close(){generation++;active?.close();}
 window.LockCluster?.on('effective',value=>{if(value)close();});
 window.addEventListener('pagehide',close);
 function button({sessionId,runId,api,blocked,t,error}){
  const launch=node('button','quiet outcome-launch',t('成果舱'));launch.type='button';launch.setAttribute('aria-haspopup','dialog');
  launch.onclick=async()=>{
   if(blocked?.())return;
   close();const gen=++generation,dialog=node('dialog','outcome-dialog');active=dialog;
   const head=node('header','outcome-head'),heading=node('div');const title=node('h2','',t('任务成果舱'));title.id='outcome-title';dialog.setAttribute('aria-labelledby',title.id);heading.append(node('span','sheet-eyebrow','TASK OUTCOME'),title);
   const dismiss=node('button','icon-button','×');dismiss.type='button';dismiss.setAttribute('aria-label',t('关闭成果舱'));dismiss.onclick=()=>dialog.close();head.append(heading,dismiss);
   const content=node('div','outcome-content'),notice=node('p','muted',t('正在读取任务记录…'));
   content.append(notice);dialog.append(head,content);document.body.append(dialog);
   dialog.addEventListener('close',()=>{if(active===dialog){active=null;generation++;}dialog.remove();launch.focus();},{once:true});dialog.showModal();
   const valid=()=>gen===generation&&dialog.open&&!blocked?.();
   const base='/sessions/'+encodeURIComponent(sessionId)+'/runs/'+encodeURIComponent(runId)+'/outcome';
   let data,files=[],executions=[];
   const section=(title)=>{const s=node('section','outcome-section');s.append(node('h3','',t(title)));content.append(s);return s;};
   async function evidence(record){
    if(!valid())return;
    try{const raw=await api(base+'/evidence/'+encodeURIComponent(record.id)+'?digest='+encodeURIComponent(record.digest));if(!valid())return;
     const details=node('details','outcome-evidence');details.open=true;details.append(node('summary','',record.id+' · '+raw.tool),node('h4','',t('原始参数')),node('pre','',raw.args||'—'),node('h4','',t('原始返回')),node('pre','',raw.result||t('没有返回记录')),node('code','outcome-digest',raw.digest));
     const previous=content.querySelector('.outcome-evidence');previous?.remove();content.append(details);details.scrollIntoView({block:'nearest',behavior:matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'});
    }catch(e){if(valid())error?.(e.message);}
   }
   function render(){
    content.replaceChildren();const intro=node('div','outcome-intro');intro.append(node('p','muted','#'+(data.sessionNumber||'?')+' · '+data.sessionTitle),node('p','',data.goal),node('code','outcome-digest',data.taskId),node('p','muted',t('任务状态')+' · '+data.status+' / '+t('工作区')+' · '+(data.workspaceId||'—')));if(data.goalTruncated)intro.append(node('p','muted',t('目标摘要已截断，原文摘要指纹保留在导出记录中')));content.append(intro);
    const metrics=node('div','outcome-metrics');for(const [key,label]of [['files','文件提案'],['applicationRecords','应用记录'],['executions','执行返回'],['verificationReports','验证报告'],['unknownOutcomes','未知结果']]){const cell=node('div');cell.append(node('strong','',data.summary[key]),node('span','',t(label)));metrics.append(cell);}content.append(metrics);
    content.append(node('p','outcome-boundary',t('任务状态、模型计划与验证报告不是独立验收证明；展开证据查看原始返回。')));
    if(data.plan?.items?.length){const s=section('目标与计划');for(const item of data.plan.items)s.append(node('p','',item.step+' · '+item.status+(item.evidence?.length?' · '+item.evidence.map(i=>'E'+String(i).padStart(4,'0')).join(', '):'')));}
    const fs=section('文件变更');if(!files.length)fs.append(node('p','muted',t('没有文件提案记录')));for(const f of files){const row=node('div','outcome-file');row.append(node('code','',f.path),node('span','muted',t(f.state==='proposed'?'提案未应用':'应用已有记录，当前文件未复核')),node('small','outcome-digest',(f.beforeHash||'—')+' → '+f.proposedHash),node('small','muted',f.proposedBytes+' bytes'));fs.append(row);}
    const es=section('执行与证据');if(!executions.length)es.append(node('p','muted',t('没有工具返回记录')));for(const record of executions){const row=node('details','outcome-execution'),summary=node('summary');summary.append(node('code','',record.id),node('span','',record.tool),node('small','muted',t(record.state==='no_result'?'没有返回记录':'已记录返回')));row.append(summary,node('pre','',record.preview||'—'));if(record.truncated)row.append(node('p','muted',t('预览已截断，可展开原始证据')));const b=node('button','quiet',t('查看原始证据'));b.onclick=()=>evidence(record);row.append(b,node('small','outcome-digest',record.digest));es.append(row);}
    if(data.page.filesMore||data.page.executionsMore){const more=node('button','quiet',t('加载更多成果记录'));more.onclick=async()=>{more.disabled=true;try{const next=await api(base+'?fileOffset='+data.page.fileNext+'&executionOffset='+data.page.executionNext);if(!valid())return;files.push(...next.files);executions.push(...next.executions);data={...next,files,executions};render();}catch(e){if(valid()){error?.(e.message);more.disabled=false;}}};es.append(more);}
    const approvals=section('审批依据');approvals.append(node('p','muted',data.approvalNote||t('历史记录可能缺少审批来源')));for(const review of data.approvals||[]){const entry=node('details'),source={model:'模型独立审核',human:'人工确认',remembered:'授权规则匹配'};entry.append(node('summary','',t(source[review.source]||'历史来源未记录')+' · '+t(({approved:'已自动放行',confirmed:'人工已确认',declined:'人工未批准',reviewing:'审核中',manual:'需手动确认',aborted:'未自动放行'})[review.status]||'历史状态未记录')),node('p','',review.reason),node('pre','',review.command),node('p','muted',(review.at||'')+' · '+(review.workspace||'—')+' · '+(review.root?.displayHost||review.root?.containerAbs||'—')),node('code','outcome-digest',review.fingerprint||'—'));if(review.ruleId)entry.append(node('p','muted',review.ruleId+' · '+(review.expiresAt||t('永久'))));approvals.append(entry);}
    const vs=section('验证记录');if(!data.verification.length)vs.append(node('p','muted',t('尚无验证报告；不推断通过')));for(const report of data.verification){const d=node('details');d.append(node('summary','',report.title+' · '+report.evidence+' · '+t('报告声明')),node('pre','',report.content));if(report.truncated)d.append(node('p','muted',t('报告摘要已截断，原文见对应执行证据')));vs.append(d);}
    if(data.findings?.length){const rs=section('来源与发现');for(const f of data.findings){const d=node('details');d.append(node('summary','',f.name+' · '+f.value+(f.unit?' '+f.unit:'')),node('p','muted',f.kind+' · '+(f.evidence?'E'+String(f.evidence).padStart(4,'0'):t('无来源编号'))),node('blockquote','',f.quote||'—'),node('p','muted',f.limitation||t('可追溯引用不证明来源结论准确')));rs.append(d);}}
    if(data.suggestedCommands?.length){const s=section('建议命令（尚未运行）');for(const command of data.suggestedCommands)s.append(node('pre','',command));}
    if(data.unknownCalls?.length){const s=section('未取得结果的调用');for(const call of data.unknownCalls)s.append(node('p','',call.tool+' · '+(call.callId||'—')+' · '+call.state));}
    const gaps=section('缺口与发布');for(const gap of data.gaps)gaps.append(node('p','',gap));gaps.append(node('p','muted',data.release.message));
    const actions=node('div','outcome-actions');const exportButton=node('button','quiet',t('导出成果 JSON'));exportButton.onclick=()=>{if(!valid())return;const blob=new Blob([JSON.stringify({...data,files,executions,exportScope:{kind:'summary',allPagesLoaded:!data.page.filesMore&&!data.page.executionsMore,rawEvidenceIncluded:false,textTruncated:!!data.goalTruncated||executions.some(e=>e.truncated)||data.verification.some(r=>r.truncated)}},null,2)],{type:'application/json'}),url=URL.createObjectURL(blob),a=node('a');a.href=url;a.download='aide-outcome-'+runId+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};actions.append(exportButton);content.append(actions);
   }
   try{data=await api(base);if(!valid())return;files=data.files;executions=data.executions;render();}catch(e){if(valid()){content.replaceChildren(node('p','task-error',e.message));}}
  };
  return launch;
 }
 window.AideTaskOutcome={button,close};
})();
