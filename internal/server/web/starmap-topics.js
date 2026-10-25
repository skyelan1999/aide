'use strict';
window.AideKnowledgeTopics={create({request,onSaved=()=>{}}){
 const host=document.getElementById('timeline-topics');if(!host)return {sync(){}};
 const make=(tag,text,cls)=>{const n=document.createElement(tag);if(text)n.textContent=text;if(cls)n.className=cls;return n;};
 let scope=null,generation=0,controller=null,busy=false,revision=null,topics=[],candidates={before:[],after:[]};
 function sourceOffset(raw,offset){let i=0,shown=0;while(i<raw.length&&shown<offset){i+=raw[i]==='\r'&&raw[i+1]==='\n'?2:1;shown++;}return i;}
 const displayOffset=(raw,offset)=>raw.slice(0,offset).replace(/\r\n?/g,'\n').length;
 const origin=c=>[({workspace:uiTextStarmapTopics('工作目录'),context:uiTextStarmapTopics('引用目录'),source:uiTextStarmapTopics('引用来源')}[c.root]||''),c.source,c.node].filter(Boolean).join(' · ');
 const status=make('p',uiTextStarmapTopics('选择两次观察后，展开并读取历史片段。'),'source-meta');status.setAttribute('role','status');
 const reload=make('button',uiTextStarmapTopics('读取主题与历史片段'));reload.type='button';
 const form=make('form'),topic=make('select'),name=make('input'),save=make('button',uiTextStarmapTopics('保存主题对齐')),disable=make('button',uiTextStarmapTopics('停用此主题（保留标记）'));
 topic.setAttribute('aria-label',uiTextStarmapTopics('人工主题'));name.setAttribute('aria-label',uiTextStarmapTopics('主题名称'));name.maxLength=80;name.placeholder=uiTextStarmapTopics('例如：定位方案的取舍');save.type='submit';disable.type='button';
 const label=(text,control)=>{const n=make('label',text);n.append(control);return n;};
 form.append(label(uiTextStarmapTopics('人工主题'),topic),label(uiTextStarmapTopics('主题名称'),name));
 const pickers=make('div',null,'topic-pickers'),fields={};
 for(const [side,title] of [['before',uiTextStarmapTopics('此前')],['after',uiTextStarmapTopics('之后')]]){
  const box=make('fieldset'),legend=make('legend',title+uiTextStarmapTopics('的历史片段')),filter=make('input'),select=make('select'),text=make('textarea'),selected=make('button',uiTextStarmapTopics('使用选中的文字')),whole=make('button',uiTextStarmapTopics('使用整段')),note=make('p',uiTextStarmapTopics('尚未选择文字'),'source-meta');
  filter.maxLength=128;filter.setAttribute('aria-label',title+uiTextStarmapTopics('节点编号筛选'));filter.placeholder=uiTextStarmapTopics('节点编号（可选），填入后重新读取');select.setAttribute('aria-label',title+uiTextStarmapTopics('历史片段'));text.setAttribute('aria-label',title+uiTextStarmapTopics('历史原文'));text.readOnly=true;
  selected.type=whole.type='button';box.append(legend,label(uiTextStarmapTopics('按节点编号筛选'),filter),label(uiTextStarmapTopics('历史来源'),select),text,selected,whole,note);pickers.append(box);
  const field={box,filter,select,text,note,pick:null};fields[side]=field;
  function choose(start,end){const c=candidates[side].find(c=>c.token===select.value);if(!c||end<=start||!text.value.slice(start,end).trim()){status.textContent=uiTextStarmapTopics('请先选择来源，并选中非空文字，或使用整段。');return;}
   field.pick={snapshot:scope[side],node:c.node,candidate:c.token,start:sourceOffset(c.quote.text,start),end:sourceOffset(c.quote.text,end)};note.textContent=uiTextStarmapTopics("已选择 {0} 个字符 · {1} · {2}", Array.from(text.value.slice(start,end)).length, c.quote.partial?uiTextStarmapTopics('原文仅部分覆盖'):uiTextStarmapTopics('当次保存的片段'), origin(c));update();}
  selected.onclick=()=>choose(text.selectionStart,text.selectionEnd);whole.onclick=()=>choose(0,text.value.length);
  select.onchange=()=>{const c=candidates[side].find(c=>c.token===select.value);text.value=c?.quote.text||'';field.pick=null;note.textContent=c?uiTextStarmapTopics('请选中文字，或明确使用整段。 · ')+origin(c):uiTextStarmapTopics('尚未选择文字');update();};
 }
 const actions=make('div',null,'topic-actions');actions.append(save,disable);form.append(pickers,actions);
 const archived=make('details'),archiveTitle=make('summary',uiTextStarmapTopics('保留的旧标记（观察已淘汰）')),archiveBody=make('div');archived.append(archiveTitle,archiveBody);archived.hidden=true;form.append(archived);
 const intro=make('p',uiTextStarmapTopics('人工对齐只记录你选择的主题关系，不证明结论正确或模型读过依据。依据仅比较所选片段内的明确引用；原文不改写，历史不会变成当前来源。'),'source-meta');host.append(intro,reload,status,form);
 function update(){reload.disabled=busy||!scope;topic.disabled=name.disabled=busy||revision===null;save.disabled=busy||revision===null||!name.value.trim()||!fields.before.pick||!fields.after.pick;disable.disabled=busy||revision===null||!topic.value||!!topics.find(t=>t.id===topic.value)?.disabled;
  for(const f of Object.values(fields)){f.box.disabled=busy||!scope;f.select.disabled=f.filter.disabled=busy||!scope;}
 }
 function topicOptions(selected=''){topic.replaceChildren();const first=make('option',uiTextStarmapTopics('新建主题'));first.value='';topic.append(first);for(const t of topics){const o=make('option',t.label+(t.disabled?uiTextStarmapTopics('（已停用）'):'')+(t.expiredMembers?' · '+t.expiredMembers+uiTextStarmapTopics('次观察已淘汰'):''));o.value=t.id;topic.append(o);}topic.value=topics.some(t=>t.id===selected)?selected:'';}
 function candidateOptions(side){const f=fields[side],old=f.pick;f.select.replaceChildren();const blank=make('option',uiTextStarmapTopics('选择历史片段'));blank.value='';f.select.append(blank);
  for(const c of candidates[side]){const o=make('option',`${c.path||uiTextStarmapTopics('会话 ')+c.session} · ${c.quote.heading||uiTextStarmapTopics('未标记结论')}${c.ai?uiTextStarmapTopics(' · 任务 ')+c.ai.run:''}${c.quote.partial?uiTextStarmapTopics(' · 部分覆盖'):''}`);o.value=c.token;f.select.append(o);}
  f.pick=null;f.text.value='';f.note.textContent=candidates[side].length?uiTextStarmapTopics('请选择片段并选中文字。'):uiTextStarmapTopics('当前范围没有可选片段，可按节点编号重新读取。');
  if(old){const c=candidates[side].find(c=>c.token===old.candidate);if(c){f.select.value=c.token;f.text.value=c.quote.text;f.pick=old;f.note.textContent=uiTextStarmapTopics('已保留所选片段。');}}
 }
 function applyTopic(){const t=topics.find(t=>t.id===topic.value);name.value=t?.label||'';
  archiveBody.replaceChildren();archived.hidden=!t?.archived?.length;for(const m of t?.archived||[]){archiveBody.append(make('p',uiTextStarmapTopics("{0} · {1} · 观察已淘汰，仅保留操作者选定原话，无法重新核验完整依据。", m.pick.snapshot, m.candidate.path||uiTextStarmapTopics('会话 ')+m.candidate.session),'source-meta'),make('pre',m.candidate.quote.text));}
  for(const side of ['before','after']){const f=fields[side],m=t?.members.find(m=>m.pick.snapshot===scope?.[side]);f.pick=null;f.text.value='';f.select.value='';f.note.textContent=uiTextStarmapTopics('尚未选择文字');if(m){const c=candidates[side].find(c=>c.token===m.pick.candidate);if(c){f.select.value=c.token;f.text.value=c.quote.text;f.pick={...m.pick};f.text.setSelectionRange(displayOffset(c.quote.text,m.pick.start),displayOffset(c.quote.text,m.pick.end));f.note.textContent=uiTextStarmapTopics('已恢复人工标记的片段。 · ')+origin(c);}else{f.filter.value=m.pick.node;f.note.textContent=uiTextStarmapTopics('此标记未在当前片段列表中；已填入节点编号，请重新读取。');}}}
  update();
 }
 function cancel(){generation++;controller?.abort();controller=null;busy=false;}
 async function call(method,body){cancel();const gen=generation,current=scope,control=new AbortController();controller=control;busy=true;update();const timeout=setTimeout(()=>control.abort(),10000);
  const query=new URLSearchParams({workspace:current.workspace,...(current.code?{code:'1'}:{})});if(method==='GET'){query.set('before',current.before);query.set('after',current.after);for(const side of ['before','after'])if(fields[side].filter.value.trim())query.set(side+'Node',fields[side].filter.value.trim());}
  try{const data=await request('/timeline/topics?'+query.toString(),{method,signal:control.signal,...(body?{body:JSON.stringify(body)}:{})});if(gen!==generation||scope!==current)return null;if(control.signal.aborted)throw Object.assign(Error('aborted'),{name:'AbortError'});return data;}
  catch(error){if(gen===generation&&scope===current)status.textContent=error.name==='AbortError'?(method==='PUT'?uiTextStarmapTopics('保存结果尚未确认，请重新读取核对。'):uiTextStarmapTopics('读取已取消或超时，请重试。')):error.message;return null;}
  finally{clearTimeout(timeout);if(gen===generation){controller=null;busy=false;update();}}
 }
 async function load(){if(!scope)return;status.textContent=uiTextStarmapTopics('正在读取人工主题和已保存原文…');const selected=topic.value,data=await call('GET');if(!data)return;revision=data.revision;topics=data.topics;candidates={before:data.before,after:data.after};topicOptions(selected);for(const side of ['before','after'])candidateOptions(side);if(topic.value)applyTopic();status.textContent=uiTextStarmapTopics("{0} 个主题 · {1}原文未完整保存的部分不能选择。", topics.length, data.partial?uiTextStarmapTopics('片段列表仅部分覆盖；可按节点编号筛选。'):uiTextStarmapTopics('历史片段已读取。'));update();}
 reload.onclick=load;topic.onchange=applyTopic;name.oninput=update;
 form.onsubmit=async event=>{event.preventDefault();if(save.disabled||!scope)return;status.textContent=uiTextStarmapTopics('正在保存人工主题…');const data=await call('PUT',{expected:revision,id:topic.value,action:'upsert',label:name.value.trim(),before:fields.before.pick,after:fields.after.pick});if(!data)return;revision=data.revision;topics=data.topics;topicOptions(data.id);status.textContent=uiTextStarmapTopics('主题已保存。请重新比较这两次观察。');update();onSaved();};
 disable.onclick=async()=>{if(disable.disabled||!scope)return;const data=await call('PUT',{expected:revision,id:topic.value,action:'disable'});if(!data)return;revision=data.revision;topics=data.topics;topicOptions(data.id);status.textContent=uiTextStarmapTopics('主题已停用，原标记保留；重新保存可恢复对齐。');update();onSaved();};
 host.addEventListener('toggle',()=>{if(host.open&&scope&&revision===null)load();});addEventListener('pagehide',cancel);
 update();return {sync(next){if(scope?.workspace===next?.workspace&&scope?.code===next?.code&&scope?.before===next?.before&&scope?.after===next?.after)return;cancel();scope=next;revision=null;topics=[];candidates={before:[],after:[]};topicOptions();name.value='';archiveBody.replaceChildren();archived.hidden=true;for(const side of ['before','after']){fields[side].filter.value='';candidateOptions(side);}status.textContent=next?uiTextStarmapTopics('展开后读取历史片段。'):uiTextStarmapTopics('请先选择两次观察。');update();if(host.open&&scope)load();}};
}};

function uiTextStarmapTopics(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? key; }
