'use strict';
(function () {
 const definitions=[
  {id:'send',label:'发送消息',binding:'Shift+Enter'},
  {id:'search',label:'搜索会话',binding:'Mod+K'},
  {id:'newSession',label:'新建会话',binding:'N'},
  {id:'settings',label:'打开设置',binding:'Mod+Comma'},
  {id:'focusPrompt',label:'聚焦消息输入框',binding:''},
  {id:'files',label:'切换文件面板',binding:''},
  {id:'plugins',label:'切换插件面板',binding:''},
  {id:'trajectory',label:'查看会话轨迹',binding:''},
  {id:'terminal',label:'切换命令面板',binding:''},
  {id:'stop',label:'停止本轮',binding:''}
 ];
 const mac=/Mac|iPhone|iPad/.test(navigator.platform||'');
 const modifiers=['Mod','Ctrl','Meta','Alt','Shift'];
 const keys=/^(?:[A-Z0-9]|Enter|Space|Comma|Period|Slash|Backslash|Semicolon|Quote|Minus|Equal|BracketLeft|BracketRight|ArrowUp|ArrowDown|ArrowLeft|ArrowRight|Home|End|PageUp|PageDown|F(?:[1-9]|1[0-2]))$/;
 const codeAliases={Space:'Space',Comma:'Comma',Period:'Period',Slash:'Slash',Backslash:'Backslash',Semicolon:'Semicolon',Quote:'Quote',Minus:'Minus',Equal:'Equal',BracketLeft:'BracketLeft',BracketRight:'BracketRight'};
 let actions={},blocked=()=>false,recording=null;
 function physical(binding){return binding.split('+').map(k=>k==='Mod'?(mac?'Meta':'Ctrl'):k).join('+');}
 function valid(binding,id){
  if(binding==='')return true;
  if(typeof binding!=='string')return false;
  const parts=binding.split('+'),key=parts.pop();
  if(!keys.test(key)||parts.some((p,i)=>!modifiers.includes(p)||parts.indexOf(p)!==i)||parts.join('+')!==modifiers.filter(m=>parts.includes(m)).join('+'))return false;
  if(parts.includes('Mod')&&(parts.includes('Ctrl')||parts.includes('Meta')))return false;
  if(id==='send')return key==='Enter'&& !parts.includes('Alt');
  if(id==='newSession'&&binding==='N')return true;
  // Global letter bindings need a command modifier; ordinary typing stays ordinary typing.
  if(!parts.some(p=>['Mod','Ctrl','Meta','Alt'].includes(p)))return false;
  const actual=physical(binding).split('+');
  const command=actual.includes('Meta')||actual.includes('Ctrl');
  if(actual.includes('Alt')&&!command&&['F4','Space'].includes(key))return false;
  if(command&&!actual.includes('Alt')&&['A','C','V','X','Z','Y','F','Q','W','R','L','T','N','H','M','Space'].includes(key))return false;
  return true;
 }
 function getAll(){
  const stored=window.aideUI?.get('keybindings'),result={};
  for(const d of definitions){const value=stored&&Object.hasOwn(stored,d.id)?stored[d.id]:d.binding;result[d.id]=valid(value,d.id)?value:d.binding;}
  // Corrupt/imported duplicate bindings must not fire two actions.
  const seen=new Set();for(const d of definitions){const p=physical(result[d.id]);if(p&&seen.has(p))result[d.id]='';else if(p)seen.add(p);}
  return result;
 }
 function eventBinding(event){
  if(event.isComposing||event.keyCode===229||event.getModifierState?.('AltGraph'))return '';
  const key=/^Key[A-Z]$/.test(event.code||'')?event.code.slice(3):/^Digit[0-9]$/.test(event.code||'')?event.code.slice(5):codeAliases[event.code]||String(event.key||'').toUpperCase();
  const named={ENTER:'Enter',' ':'Space',ARROWUP:'ArrowUp',ARROWDOWN:'ArrowDown',ARROWLEFT:'ArrowLeft',ARROWRIGHT:'ArrowRight',HOME:'Home',END:'End',PAGEUP:'PageUp',PAGEDOWN:'PageDown',',':'Comma'};
  const normalized=named[key]||key;if(!keys.test(normalized))return '';
  return [event.ctrlKey?'Ctrl':'',event.metaKey?'Meta':'',event.altKey?'Alt':'',event.shiftKey?'Shift':'',normalized].filter(Boolean).join('+');
 }
 function matches(id,event){const binding=getAll()[id];return !!binding&&physical(binding)===eventBinding(event);}
 function format(binding){
  const names={Mod:mac?'⌘':'Ctrl',Meta:mac?'⌘':'Meta',Ctrl:mac?'⌃':'Ctrl',Alt:mac?'⌥':'Alt',Shift:mac?'⇧':'Shift',Comma:',',Period:'.',Slash:'/',Backslash:'\\',Semicolon:';',Quote:"'",Minus:'−',Equal:'=',BracketLeft:'[',BracketRight:']',Space:'Space',ArrowUp:'↑',ArrowDown:'↓',ArrowLeft:'←',ArrowRight:'→'};
  return binding?binding.split('+').map(k=>names[k]||k).join(' + '):'';
 }
 function set(id,binding){
  if(!definitions.some(d=>d.id===id)||!valid(binding,id))return {error:'请选择组合键；发送消息仅支持 Enter 及其修饰键，常用系统快捷键不可覆盖。'};
  const conflict=definitions.find(d=>d.id!==id&&binding&&physical(getAll()[d.id])===physical(binding));
  if(conflict)return {conflict:conflict.label};
  const stored=window.aideUI?.get('keybindings');window.aideUI?.set('keybindings',{...(stored&&typeof stored==='object'&&!Array.isArray(stored)?stored:{}),[id]:binding});return {};
 }
 function hint(t){const binding=getAll().send;return binding?t('{0} 发送 · {1} 换行',format(binding),binding==='Enter'?'Shift + Enter':'Enter'):t('发送快捷键已禁用 · Enter 换行');}
 function render({t,onCleanup}){
  const make=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text)n.textContent=text;return n;};
  const wrap=make('div','keybindings-settings'),intro=make('p','section-desc',t('点击快捷键后按下新组合键，立即保存到当前设备。Esc 取消录入；系统和编辑器内置快捷键保留。'));
  const status=make('p','keybindings-status');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
  const rows=[];
  const cancel=()=>{recording=null;status.textContent=t('已取消录入');sync();};
  const sync=()=>{const values=getAll();for(const row of rows){row.key.textContent=recording?.id===row.d.id?t('请按下组合键…'):format(values[row.d.id])||t('未绑定');row.key.classList.toggle('recording',recording?.id===row.d.id);row.key.setAttribute('aria-pressed',String(recording?.id===row.d.id));row.disable.disabled=!values[row.d.id];}};
  const list=make('div','keybindings-list');
  for(const d of definitions){
   const row=make('div','keybindings-row'),copy=make('div','keybindings-copy');copy.append(make('strong','',t(d.label)),make('small','muted',d.id==='send'?t('仅在消息输入框内生效'):t('工作台快捷操作')));
   const controls=make('div','keybindings-actions'),key=make('button','keybinding-key'),disable=make('button','quiet',t('禁用')),reset=make('button','quiet',t('恢复默认'));
   for(const b of [key,disable,reset])b.type='button';key.setAttribute('aria-label',t('修改{0}快捷键',t(d.label)));disable.setAttribute('aria-label',t('禁用{0}快捷键',t(d.label)));reset.setAttribute('aria-label',t('恢复{0}默认快捷键',t(d.label)));
   const apply=binding=>{const result=set(d.id,binding);status.textContent=result.conflict?t('与“{0}”冲突，请选择其他组合键。',t(result.conflict)):result.error?t(result.error):t('快捷键已保存');if(!result.conflict&&!result.error)recording=null;sync();};
   key.onclick=()=>{recording={id:d.id,element:key,apply,cancel:()=>{recording=null;status.textContent=t('已取消录入');sync();}};status.textContent=t('正在录入“{0}”，按 Esc 取消。',t(d.label));sync();};
   key.onblur=()=>{if(recording?.element===key)cancel();};disable.onclick=()=>apply('');reset.onclick=()=>apply(d.binding);
   rows.push({d,key,disable});controls.append(key,disable,reset);row.append(copy,controls);list.append(row);
  }
  const resetAll=make('button','quiet',t('恢复全部默认快捷键'));resetAll.type='button';resetAll.onclick=()=>{recording=null;window.aideUI?.set('keybindings',{});status.textContent=t('已恢复默认快捷键');sync();};
  const unsub=window.aideUI?.subscribe(sync);onCleanup?.(()=>{if(recording&&wrap.contains(recording.element))recording=null;unsub?.();});
  wrap.append(intro,list,status,resetAll);sync();return wrap;
 }
 function dispatch(event){
  if(recording&&blocked()){recording.cancel();return;}
  if(recording){
   if(!recording.element.isConnected){recording=null;return;}
   if(event.key==='Tab'){recording.cancel();return;}
   event.preventDefault();event.stopImmediatePropagation();
   if(event.key==='Escape'){recording.cancel();return;}
   const binding=eventBinding(event);if(binding)recording.apply(binding);return;
  }
  if(event.defaultPrevented||event.repeat||blocked()||!eventBinding(event))return;
  const modal=document.querySelector('dialog[open],.workspace-sheet.open,.trajectory-sheet.open,.settings-sheet.open');
  if(modal)return;
  const editable=event.target?.closest?.('input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"]');
  for(const d of definitions){
   if(d.id==='send'||!matches(d.id,event))continue;
   if(editable&&!event.ctrlKey&&!event.metaKey&&!event.altKey)return;
   if(d.id==='newSession'&&getAll().newSession==='N'&&!['BODY','HTML'].includes(document.activeElement?.tagName))return;
   if(actions[d.id]?.()===false)return;
   event.preventDefault();return;
  }
 }
 document.addEventListener('keydown',dispatch,true);
 window.AideKeybindings={definitions,getAll,matches,format,set,hint,render,valid,eventBinding,install(options){actions=options.actions;blocked=options.blocked||(()=>false);}};
})();
