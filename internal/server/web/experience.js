/* Decorative startup and page lifecycle; never delays application initialization. */
'use strict';
(function(){
 let intro=null,timer=0,dismissal=null;
 const root=document.documentElement;
 const motionMedia=window.matchMedia?.('(prefers-reduced-motion: reduce)');
 function motionAllowed(){return !document.hidden&&root.dataset.motion!=='reduced'&&!motionMedia?.matches;}
 function remove(){clearTimeout(timer);dismissal?.cancel();dismissal=null;if(intro){intro.remove();intro=null;}document.removeEventListener('pointerdown',dismiss);document.removeEventListener('keydown',dismiss);}
 function dismiss(){
  if(!intro||dismissal)return;
  if(!motionAllowed()||typeof intro.animate!=='function'){remove();return;}
  clearTimeout(timer);document.removeEventListener('pointerdown',dismiss);document.removeEventListener('keydown',dismiss);
  dismissal=intro.animate([{opacity:getComputedStyle(intro).opacity},{opacity:0}],{duration:120,easing:'ease-out',fill:'forwards'});
  dismissal.finished.then(remove,()=>{});
 }
 function replay(manual){
  remove();
  if(!motionAllowed()||location.pathname!=='/'||location.hash.startsWith('#file'))return;
  if(!manual&&window.aideUI?.get('startupAnimation')==='off')return;
  intro=document.createElement('div');intro.className='aide-intro';intro.setAttribute('aria-hidden','true');
  intro.innerHTML='<div class="aide-intro-mark"><svg viewBox="0 0 260 260" aria-hidden="true"><circle class="aide-intro-register" cx="130" cy="130" r="121" stroke-dasharray="1 12"/><path class="aide-intro-register" d="M130 1v13m0 232v13M1 130h13m232 0h13"/><path class="aide-intro-halo" d="M65 65a92 92 0 0 1 150 91"/><ellipse class="aide-intro-ring" cx="130" cy="130" rx="112" ry="70"/><circle class="aide-intro-ring inner" cx="130" cy="130" r="76"/><circle class="aide-intro-ray" cx="130" cy="130" r="95"/><path class="aide-intro-star" d="M208 55l2 7 7 2-7 2-2 7-2-7-7-2 7-2z"/></svg><span class="aide-intro-core">a<span>·</span></span></div>';
  document.body.appendChild(intro);timer=setTimeout(remove,980);
  document.addEventListener('pointerdown',dismiss,{once:true});document.addEventListener('keydown',dismiss,{once:true});
 }
 function visibility(){root.toggleAttribute('data-ui-paused',document.hidden);if(document.hidden)remove();}
 document.addEventListener('visibilitychange',visibility);window.addEventListener('aide:motion',()=>{if(root.dataset.motion==='reduced')remove();});
 window.aideExperience={replay:()=>replay(true)};visibility();
 try{if(!sessionStorage.getItem('aide.intro.shown')){sessionStorage.setItem('aide.intro.shown','1');replay(false);}}catch(_){replay(false);}
})();

/* Observe only navigation surfaces. Streaming transcripts, rows and authentication
   screens are deliberately outside this layer; visibility/focus changes stay immediate. */
(function(){
 const root=document.documentElement;
 const media=window.matchMedia?.('(prefers-reduced-motion: reduce)');
 const finePointer=window.matchMedia?.('(hover: hover) and (pointer: fine)');
 const nativeDialogMotion=!!window.CSS?.supports('transition-behavior','allow-discrete');
 const surfaces=new Map(),running=new Map(),exits=new Map();
 const sideIDs=new Set(['file-panel','plugin-panel','reminder-center']);
 const dialogIDs=new Set(['settings-dialog','editor-dialog','rca-dialog','new-file-dialog','new-folder-dialog','file-properties-dialog','plugin-settings-dialog','plugin-upload-dialog','ws-browse-dialog','source-dialog','reminder-alert']);
 const fixedIDs=new Set([...sideIDs,'strategy-menu','search-results','terminal-body','reminder-form']);
 const dynamicSelector='.session-menu,.file-ctx-menu';
 let scheduled=0,panelTransition=null,resumeSamples=new WeakMap();
 function allowed(){
  return !document.hidden&&root.dataset.motion!=='reduced'&&!media?.matches&&
   !document.querySelector('#login-dialog[open],#lock-screen:not([hidden])');
 }
 function visible(node){
  if(!node.isConnected||node.closest('[hidden],.hidden,[data-ui-exiting="true"]'))return false;
  const style=getComputedStyle(node);
  return style.display!=='none'&&style.visibility!=='hidden'&&node.getClientRects().length>0;
 }
 function cancel(node){const animation=running.get(node);if(animation){running.delete(node);animation.cancel();}}
 function settle(node){exits.get(node)?.();cancel(node);}
 function reopen(node){
  const style=exits.has(node)?getComputedStyle(node):null;
  const from=style?{opacity:style.opacity,transform:style.transform}:null;
  settle(node);
  if(from&&allowed()){
   resumeSamples.set(node,from);
   const state=surfaces.get(node);if(state)state.shown=false;
   queue();
  }
 }
 function settlePanels(){panelTransition?.();}
 function cancelAll(){resumeSamples=new WeakMap();settlePanels();for(const node of exits.keys())settle(node);for(const node of running.keys())cancel(node);}
 function reveal(node,kind='menu',from=null){
  const interrupted=running.has(node)?getComputedStyle(node):null;
  const current=from||resumeSamples.get(node)||(interrupted?{opacity:interrupted.opacity,transform:interrupted.transform}:null);
  resumeSamples.delete(node);
  cancel(node);
  if(!allowed()||!visible(node)||typeof node.animate!=='function')return;
  // A coarse pointer gets opacity only; no moving tap target or animated dimensions.
  const shifted=finePointer?.matches&&kind!=='terminal';
  const first={opacity:current?.opacity??0},last={opacity:1};
  if(shifted){first.transform=current?.transform??(kind==='side'?'translateX(8px)':'translateY(5px)');last.transform='none';}
  const animation=node.animate([first,last],{duration:kind==='side'?200:160,easing:'cubic-bezier(.22,1,.36,1)'});
  running.set(node,animation);
  animation.finished.then(()=>{if(running.get(node)===animation)running.delete(node);},()=>{});
 }
 // Logical close happens now; this retains the same inert node's paint for at
 // most 200 ms, without a DOM clone or an active button/focus gate underneath.
 function leave(node,close,{remove=false,kind='menu',snapshot=null}={}){
  if(!node){close?.();return ()=>{};}
  if(exits.has(node)){close?.();return exits.get(node);}
  const shown=!!snapshot||visible(node),style=snapshot||(shown?getComputedStyle(node):null);
  const from=style?{opacity:style.opacity,transform:style.transform}:null;
  const display=style?.display;
  cancel(node);
  if(!shown||!allowed()||typeof node.animate!=='function'){close?.();if(remove)node.remove();return ()=>{};}
  const saved={display:node.style.getPropertyValue('display'),priority:node.style.getPropertyPriority('display'),inert:node.inert,aria:node.getAttribute('aria-hidden')};
  node.inert=true;node.setAttribute('aria-hidden','true');node.dataset.uiExiting='true';
  close?.();
  if(!node.isConnected){node.inert=saved.inert;if(saved.aria===null)node.removeAttribute('aria-hidden');else node.setAttribute('aria-hidden',saved.aria);delete node.dataset.uiExiting;return ()=>{};}
  node.style.setProperty('display',display,'important');
  let timer=0,done=false;
  const finish=()=>{
   if(done)return;done=true;clearTimeout(timer);exits.delete(node);cancel(node);
   if(saved.display)node.style.setProperty('display',saved.display,saved.priority);else node.style.removeProperty('display');
   node.inert=saved.inert;if(saved.aria===null)node.removeAttribute('aria-hidden');else node.setAttribute('aria-hidden',saved.aria);
   delete node.dataset.uiExiting;if(remove)node.remove();
  };
  exits.set(node,finish);
  const last={opacity:0};
  if(finePointer?.matches&&kind!=='side'){from.transform=from.transform||'none';last.transform='translateY(3px)';}
  else delete from.transform;
  const animation=node.animate([from,last],{duration:kind==='side'?180:140,easing:'cubic-bezier(.4,0,1,1)',fill:'forwards'});
  running.set(node,animation);animation.finished.then(finish,()=>{});timer=setTimeout(finish,200);
  return finish;
 }
 function activePanel(){
  const body=document.body;
  if(body.classList.contains('plugins-mode'))return document.getElementById('plugin-panel');
  if(body.classList.contains('reminders-mode'))return document.getElementById('reminder-center');
  const files=document.getElementById('file-panel');
  return !body.classList.contains('files-hidden')&&(innerWidth>950||files?.classList.contains('mobile-open'))?files:null;
 }
 function changePanels(update){
  const body=document.body,gridStart=getComputedStyle(body).gridTemplateColumns;
  const before=activePanel(),wasVisible=before&&visible(before);
  const beforeStyle=wasVisible?getComputedStyle(before):null;
  const snapshot=beforeStyle?{opacity:beforeStyle.opacity,transform:beforeStyle.transform,display:beforeStyle.display}:null;
  const resume=new Map();
  for(const id of sideIDs){const node=document.getElementById(id);if(node&&exits.has(node)){const s=getComputedStyle(node);resume.set(node,{opacity:s.opacity,transform:s.transform});}}
  settlePanels();
  for(const id of sideIDs){const node=document.getElementById(id);if(node)settle(node);}
  update(); // Mode, selected button, accessibility and focus state update immediately.
  const after=activePanel(),gridEnd=getComputedStyle(body).gridTemplateColumns;
  if(before===after||!allowed()){scan();return;}
  const finishExit=wasVisible?leave(before,null,{kind:'side',snapshot}):()=>{};
  const previous=surfaces.get(before);if(previous)previous.shown=false;
  if(after){settle(after);reveal(after,'side',resume.get(after));const next=surfaces.get(after);if(next)next.shown=visible(after);}
  const start=gridStart.split(' '),end=gridEnd.split(' ');
  if(innerWidth<=950||!body.animate||start.length>3||end.length>3||![...start,...end].every(value=>/^\d+(?:\.\d+)?px$/.test(value)))return;
  // Desktop closes into a zero-width third grid track, not a floating screenshot.
  // The node's contents are clipped inside that track; mobile has no grid resize.
  while(start.length<3)start.push('0px');while(end.length<3)end.push('0px');
  let done=false,timer=0;
  const finish=()=>{if(done)return;done=true;clearTimeout(timer);panelTransition=null;cancel(body);finishExit();};
  panelTransition=finish;
  const animation=body.animate([{gridTemplateColumns:start.join(' ')},{gridTemplateColumns:end.join(' ')}],{duration:180,easing:'cubic-bezier(.22,1,.36,1)'});
  running.set(body,animation);animation.finished.then(finish,()=>{});timer=setTimeout(finish,200);
 }
 function scan(){
  scheduled=0;
  for(const [node,state] of surfaces){
   if(!node.isConnected){cancel(node);state.observer.disconnect();surfaces.delete(node);continue;}
   const shown=node.tagName==='DIALOG'?node.open:visible(node);
   if(shown!==state.shown){
    state.shown=shown;
    if(!shown){if(!exits.has(node))cancel(node);}
    else if(node.tagName!=='DIALOG'||!nativeDialogMotion)reveal(node,state.kind);
   }
  }
 }
 function queue(){
  if(!allowed()){if(scheduled)cancelAnimationFrame(scheduled);scan();return;}
  if(!scheduled)scheduled=requestAnimationFrame(scan);
 }
 function watch(node,inserted=false){
  if(surfaces.has(node))return;
  const dialog=node.tagName==='DIALOG'&&dialogIDs.has(node.id);
  if(!dialog&&!fixedIDs.has(node.id)&&!node.matches(dynamicSelector))return;
  const kind=sideIDs.has(node.id)?'side':node.id==='terminal-body'?'terminal':'menu';
  const observer=new MutationObserver(queue);
  observer.observe(node,{attributes:true,attributeFilter:['class','hidden','open']});
  if(dialog)node.dataset.uiMotion='dialog';
  surfaces.set(node,{shown:inserted?false:(dialog?node.open:visible(node)),kind,observer});
  if(inserted)queue();
 }
 for(const id of [...fixedIDs,...dialogIDs]){const node=document.getElementById(id);if(node)watch(node);}
 // Direct body insertions catch context menus and the lazily created reminder
 // center. No subtree observer runs over chat tokens or continuously updated lists.
 new MutationObserver(records=>{
  for(const record of records)for(const node of record.addedNodes){
   if(node.nodeType!==1)continue;
   watch(node,true);
   if(node.id==='reminder-center'){const form=node.querySelector('#reminder-form');if(form)watch(form);}
  }
  queue();
 }).observe(document.body,{childList:true});
 new MutationObserver(queue).observe(document.body,{attributes:true,attributeFilter:['class']});
 const content=document.getElementById('settings-content');
 if(content)new MutationObserver(records=>{
  // Settings render replaces direct children once per navigation. Async control
  // refreshes and counters update descendants and never restart this transition.
  if(records.some(record=>record.addedNodes.length)&&content.closest('.settings-sheet.open'))reveal(content,'content');
 }).observe(content,{childList:true});
 const lock=document.getElementById('lock-screen');
 if(lock)new MutationObserver(()=>{if(!lock.hidden)cancelAll();}).observe(lock,{attributes:true,attributeFilter:['hidden']});
 const login=document.getElementById('login-dialog');
 if(login)new MutationObserver(()=>{if(login.open)cancelAll();}).observe(login,{attributes:true,attributeFilter:['open']});
 function motionChanged(){
  if(!allowed()){
   cancelAll();if(scheduled)cancelAnimationFrame(scheduled);
   // Account for state changes while RAF is suspended, so returning to a tab
   // does not replay an old navigation or animate a stale background mutation.
   scan();
  }
 }
 document.addEventListener('visibilitychange',motionChanged);
 window.addEventListener('aide:motion',motionChanged);
 media?.addEventListener?.('change',motionChanged);
 // Interacting with a revealing surface settles it immediately, without cancelling
 // the application event or imposing a focus/scroll/input delay.
 for(const type of ['pointerdown','keydown','wheel'])document.addEventListener(type,event=>{
  // Panel controls retarget the current layout/opacity in changePanels; do not
  // snap their in-flight transition before the application handler can sample it.
  const panelControl=event.target instanceof Element&&event.target.closest('#files-toggle,#plugins-toggle,#reminder-center-button,#file-panel-close,#plugin-panel-close,#reminder-center-close');
  if(panelControl)return;
  settlePanels();
  for(const node of running.keys())if(node!==document.body&&node.contains(event.target))cancel(node);
 },{capture:true,passive:true});
 window.addEventListener('resize',()=>{resumeSamples=new WeakMap();settlePanels();for(const node of exits.keys())settle(node);});
 Object.assign(window.aideExperience,{leave,settle,reopen,changePanels});
 root.dataset.motionLayer='ready';
})();
