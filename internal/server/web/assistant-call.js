(() => {
  'use strict';
  // A page-owned call view over the shared assistant speech lifecycle.
  function create(hooks) {
    const t=hooks.translate||((text)=>text), now=hooks.now||Date.now;
    const root=document.createElement('section');root.className='assistant-call';
    const entry=document.createElement('button');entry.type='button';entry.className='assistant-call-start assistant-composer-action voice-btn';entry.innerHTML='<svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1L8 9.9a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.5c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.8 2.1Z"/></svg>';entry.title=t('语音通话');entry.setAttribute('aria-label',t('语音通话'));
    const panel=document.createElement('div');panel.className='assistant-call-panel';panel.hidden=true;
    panel.setAttribute('role','region');panel.setAttribute('aria-label',t('助理语音通话'));
    const orb=document.createElement('div');orb.className='assistant-call-orb';orb.setAttribute('aria-hidden','true');orb.textContent='◌';
    const body=document.createElement('div');body.className='assistant-call-body';
    const title=document.createElement('strong');title.textContent=t('原生实时通话');
    const status=document.createElement('span');status.setAttribute('role','status');
    const detail=document.createElement('small');detail.textContent=t('双向音频 · 可随时打断');
    const clock=document.createElement('span');clock.className='assistant-call-clock';clock.setAttribute('aria-label',t('通话时长'));
    const controls=document.createElement('div');controls.className='assistant-call-controls';
    const mute=document.createElement('button');mute.type='button';mute.className='quiet';
    const hangup=document.createElement('button');hangup.type='button';hangup.className='assistant-call-end';hangup.textContent=t('挂断');
    controls.append(mute,hangup);body.append(title,status,detail);panel.append(orb,body,clock,controls);root.append(entry,panel);
    let active=false,muted=false,started=0,timer=0,requestAt=null;
    const labels={requesting:'正在连接语音',listening:'正在听你说',thinking:'正在思考',speaking:'助理正在说话',watching:'等待你开口',recovering:'正在恢复连接',paused:'通话已中断'};
    function updateClock(){if(active&&requestAt!==null&&now()-requestAt>=20000){hooks.error(t('语音连接超时，请检查麦克风权限后重试'));end();return;}const seconds=Math.max(0,Math.floor((now()-started)/1000));clock.textContent=String(Math.floor(seconds/60)).padStart(2,'0')+':'+String(seconds%60).padStart(2,'0');}
    function paint(value){
      if(active&&(!hooks.valid()||value.paused||!value.enabled||(!muted&&!value.microphone))){end();return;}
      root.hidden=!hooks.valid();entry.hidden=!hooks.valid()||active;panel.hidden=!active;
      root.dataset.phase=active?value.phase:'off';
      if(active&&value.phase==='requesting'){if(requestAt===null)requestAt=now();}else requestAt=null;
      status.textContent=t(muted?'麦克风已静音':(labels[value.phase]||'等待你开口'));
      mute.disabled=value.phase==='requesting';mute.textContent=t(muted?'取消静音':'静音');mute.setAttribute('aria-pressed',String(muted));
    }
    async function start(){
      if(active||!hooks.valid())return;
      if(!hooks.supported()){hooks.error(t('当前浏览器不支持实时音频，请使用新版 Safari 或 Chrome'));return;}
      hooks.unlockAudio();active=true;muted=false;requestAt=null;started=now();updateClock();
      timer=(hooks.setInterval||setInterval)(updateClock,1000);
      // start emits requesting synchronously; do not paint the old inactive snapshot.
      try{await hooks.live().start(true);}catch(error){hooks.error(error.message);end();}
      if(active)paint(hooks.live().snapshot());
    }
    function end(){
      if(!active)return;
      active=false;muted=false;requestAt=null;(hooks.clearInterval||clearInterval)(timer);timer=0;
      // Invalidates pending recognition permissions, requests and queued speech.
      hooks.live().stop();hooks.cancelSpeech();paint(hooks.live().snapshot());
      if(!root.hidden)entry.focus();
    }
    function toggleMute(){if(!active)return;muted=!muted;hooks.live().setMicrophone(!muted);paint(hooks.live().snapshot());}
    entry.onclick=()=>void start();mute.onclick=toggleMute;hangup.onclick=end;
    root.addEventListener('keydown',event=>{if(event.key==='Escape'&&active){event.preventDefault();end();}});
    paint({enabled:false,phase:'off'});
    return Object.freeze({element:root,entry,start,end,paint,active:()=>active,caption:text=>{detail.textContent=String(text).slice(-160);}});
  }
  window.AideAssistantCall=Object.freeze({create});
})();
