'use strict';
// Requests own their UI lifetime; background indexing owns a different lifetime.
window.AideStarmapAssist={
 create({busy,scope}){
  let active=null,shown=null;
  const describe=job=>job.label+(job.updated?uiTextStarmapAssist(' · 索引已有更新，回答依据本次读取的资料，可重新探索核对。'):'');
  return {
   start(label){
    active?.controller.abort();
    const job={controller:new AbortController(),label,updated:false};
    active=shown=job;busy(true);scope(describe(job));
    job.controller.signal.addEventListener('abort',()=>{if(active===job){active=null;busy(false);}});
    return job;
   },
   current:job=>active===job&&!job.controller.signal.aborted,
   finish(job){if(active!==job)return;active=null;busy(false);},
   cancel(){const pending=active;active=null;shown=null;pending?.controller.abort();busy(false);scope('');return !!pending;},
   updated(){if(shown){shown.updated=true;scope(describe(shown));}}
  };
 },
 selection({find,docs,selected,results,question,search}){
  const nodes=find?[]:(selected?[selected]:docs?[]:results).filter(n=>n.kind!=='directory').slice(0,8);
  let query=(question||'').trim()||(search||'').trim();
  if(!query&&!find&&nodes.length)query=uiTextStarmapAssist('请解释这些资料如何协作，列出关键结论、关联和证据缺口，并引用编号。');
  if(!query)throw Error(uiTextStarmapAssist('请先输入要探索的问题。'));
  if(!find&&!docs&&!nodes.length)throw Error(uiTextStarmapAssist('先选择一个文件或搜索结果，再理解；也可以输入问题后使用“AI 知识搜索”。'));
  return {query,ids:nodes.map(n=>n.id),label:find?uiTextStarmapAssist('AI 搜索 · 当前来源'):docs?uiTextStarmapAssist('正文检索 · ')+(nodes[0]?.name||uiTextStarmapAssist('当前来源')):uiTextStarmapAssist('理解 · ')+nodes.map(n=>n.name||n.id).join('、')};
 },
 render(target,text,references,onReference){
  // Deliberately small Markdown subset. Never interpret HTML or model URLs.
  const doc=target.ownerDocument,known=new Map(references.map(r=>[r.id,r]));target.replaceChildren();
  const inline=(parent,value)=>{
   for(const part of value.split(/(\[[^\]\n]+\]|\*\*[^*\n]+\*\*|`[^`\n]+`)/g)){
    const ref=known.get(part.slice(1,-1));
    if(part.startsWith('[')&&part.endsWith(']')&&ref){const b=doc.createElement('button');b.type='button';b.className='ai-citation';b.textContent=part;b.title=ref.name||ref.id;b.onclick=()=>onReference(ref);parent.append(b);}
    else if(part.startsWith('**')&&part.endsWith('**')){const strong=doc.createElement('strong');strong.textContent=part.slice(2,-2);parent.append(strong);}
    else if(part.startsWith('`')&&part.endsWith('`')){const code=doc.createElement('code');code.textContent=part.slice(1,-1);parent.append(code);}
    else parent.append(doc.createTextNode(part));
   }
  };
  let paragraph=null,list=null,code=null;
  for(const line of String(text||'').split('\n')){
   if(/^\s*```/.test(line)){paragraph=list=null;if(code){code=null;}else{const pre=doc.createElement('pre');code=doc.createElement('code');pre.append(code);target.append(pre);}continue;}
   if(code){code.append(doc.createTextNode(line+'\n'));continue;}
   if(!line.trim()){paragraph=list=null;continue;}
   const heading=/^#{1,6}\s+(.+)$/.exec(line),item=/^\s*(?:[-*+] |\d+[.)] )(.+)$/.exec(line);
   if(heading){const h=doc.createElement('h3');inline(h,heading[1]);target.append(h);paragraph=list=null;}
   else if(item){paragraph=null;if(!list){list=doc.createElement('ul');target.append(list);}const li=doc.createElement('li');inline(li,item[1]);list.append(li);}
   else{list=null;if(!paragraph){paragraph=doc.createElement('p');target.append(paragraph);}else paragraph.append(doc.createTextNode('\n'));inline(paragraph,line);}
  }
 }
};

function uiTextStarmapAssist(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? key; }
