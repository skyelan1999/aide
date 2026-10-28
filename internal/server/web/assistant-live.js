(() => {
  'use strict';
  // Page-owned session: recognition and polling never survive stop, navigation or lock.
  function create(hooks) {
    const later=hooks.setTimeout||setTimeout, clear=hooks.clearTimeout||clearTimeout;
    let enabled=false, microphone=false, paused=false, phase='off', epoch=0, rec=null, stream=null, controller=null;
    let pollTimer=0, speechTimer=0, restartTimer=0, finalText='', interim='', busy=false, polling=false;
    let speaking=false,spokenText='',echoUntil=0,opening=0,openSerial=0;
    const normalize=text=>String(text).toLowerCase().replace(/[^\p{L}\p{N}]/gu,'');
    const echo=text=>{const value=normalize(text),spoken=normalize(spokenText);return value&&spoken&&spoken.includes(value);};
    let lastActivity=Date.now(),lastInitiativeCheck=0,initiativeController=null,autonomousAudio=false;
    const activity=()=>{lastActivity=Date.now();initiativeController?.abort();if(autonomousAudio&&speaking){autonomousAudio=false;speaking=false;notices=[];hooks.cancelSpeech?.();}};
    let baseline=false, seen=new Set(), ended=[], failureCount=0, notices=[];
    const snapshot=()=>({enabled,microphone,paused,phase,busy});
    const state=value=>{phase=value;hooks.state?.(snapshot());};
    const turnDelay=()=>hooks.turnDelay?hooks.turnDelay(finalText+interim,!!interim):1100;
    const armTurn=()=>{clear(speechTimer);speechTimer=later(()=>void submit(),turnDelay());};
    const allowed=()=>enabled&&!paused&&hooks.valid();
    function release(){
      clear(speechTimer);clear(restartTimer);speechTimer=restartTimer=0;
      if(rec){const old=rec;rec=null;old.onend=old.onresult=old.onerror=old.onstart=old.onspeechstart=old.onspeechend=null;try{old.abort();}catch(_){}}
      stream?.getTracks().forEach(track=>track.stop());stream=null;
    }
    function stop(){initiativeController?.abort();enabled=false;microphone=false;paused=false;epoch++;opening=0;controller?.abort();controller=null;busy=false;speaking=false;spokenText='';notices=[];release();clear(pollTimer);pollTimer=0;hooks.cancelSpeech?.();finalText=interim='';state('off');}
    function pause(){initiativeController?.abort();if(!enabled)return;paused=true;epoch++;opening=0;speaking=false;spokenText='';finalText=interim='';controller?.abort();controller=null;busy=false;release();clear(pollTimer);hooks.cancelSpeech?.();state('paused');}
    async function listen(){
      if(!allowed()||!microphone||(busy&&!speaking)||rec||opening)return;
      const token=epoch,openTicket=++openSerial;opening=openTicket;if(!speaking)state('requesting');
      try{
        const result=await hooks.openMicrophone();
        if(token!==epoch||!allowed()||!microphone){result?.stream?.getTracks().forEach(t=>t.stop());return;}
        stream=result?.stream||null;rec=result.recognition;const current=rec;
        current.onstart=()=>{if(token===epoch&&rec===current&&!speaking)state('listening');};
        current.onresult=event=>{
          if(token!==epoch||rec!==current||!allowed())return;
          failureCount=0;let part='';
          for(let i=event.resultIndex;i<event.results.length;i++){
            const value=event.results[i],text=value[0].transcript;
            if((speaking||Date.now()<echoUntil)&&echo(text))continue;
            if(speaking){
              if(normalize(text).length<2&&!/[停别]/.test(text))continue;
              speaking=false;echoUntil=Date.now()+1200;notices=[];hooks.cancelSpeech?.();state('listening');hooks.interrupted?.();
            }
            if(value.isFinal)finalText+=text;else part+=text;
          }
          interim=part;if(!finalText&&!interim)return;activity();hooks.transcript?.(finalText,interim);
          armTurn();
        };
        current.onspeechstart=()=>clear(speechTimer);
        current.onspeechend=()=>{armTurn();};
        current.onerror=event=>{
          if(token!==epoch||rec!==current)return;
          if(event.error==='no-speech'){return;}
          hooks.error?.(event.error==='not-allowed'||event.error==='service-not-allowed'?'麦克风权限被拒绝，请允许后重试':'语音连接中断，请点击麦克风重试');setMicrophone(false);
        };
        current.onend=()=>{
          if(token!==epoch||rec!==current||!allowed())return;
          rec=null;stream?.getTracks().forEach(t=>t.stop());stream=null;
          if((finalText.trim()||interim.trim())&&!busy){if(hooks.deferRecognitionEnd?.()){armTurn();restartTimer=later(()=>void listen(),400);return;}void submit();return;}
          const now=Date.now();ended=ended.filter(at=>now-at<60000);ended.push(now);
          if(ended.length>5){hooks.error?.('语音连接频繁中断，请点击麦克风重试');setMicrophone(false);return;}
          restartTimer=later(()=>void listen(),400);
        };
        try{const track=stream?.getAudioTracks?.()[0];track?current.start(track):current.start();}
        catch(error){if(!stream)throw error;stream.getTracks().forEach(t=>t.stop());stream=null;current.start();}
      }catch(error){if(token!==epoch)return;release();hooks.error?.(error.message||'无法启动语音会话');setMicrophone(false);}
      finally{if(opening===openTicket)opening=0;}
    }
    async function submit(){
      if(!allowed()||busy)return;
      const text=(finalText+interim).trim();if(!text)return;
      finalText=interim='';release();busy=true;const token=epoch;controller=new AbortController();state('thinking');
      try{
        const result=await hooks.send(text,controller.signal);
        if(token!==epoch||!allowed())return;
        if(result?.action==='locked'){hooks.error?.(result.reason);stop();return;}
        await hooks.reply?.(result,text);
        if(token!==epoch||!allowed())return;
        if(result?.reply&&result.action!=='silent'&&result.action!=='ignore'){
          await speak(result.reply);
        }
      }catch(error){if(token===epoch&&error.name!=='AbortError'){hooks.error?.(error.message);pause();}}
      finally{if(token===epoch){busy=false;controller=null;settle();}}
    }
    function settle(){if(!allowed()||busy)return;
      if(finalText||interim){armTurn();return;}
      if(notices.length)void announce();else if(microphone){if(rec)state('listening');else void listen();}else state('watching');
    }
    async function speak(text){
      const token=epoch;speaking=true;spokenText=text;finalText=interim='';
      state('speaking');if(microphone)await listen();
      if(token!==epoch||!allowed()||!speaking)return;
      try{await hooks.speak?.(text);}finally{if(token===epoch){speaking=false;echoUntil=Date.now()+1200;lastActivity=Date.now();}}
    }
    async function announce(){
      if(!allowed()||busy||!notices.length)return;
      const token=epoch;const text=notices.splice(0,5).join(' ');busy=true;
      try{await speak(text);}catch(error){if(token===epoch)hooks.error?.(error.message);}
      finally{if(token===epoch){busy=false;settle();}}
    }
    async function poll(){
      if(!allowed()){if(enabled&&!hooks.valid())stop();return;}
      if(!polling){polling=true;const token=epoch;try{
        const events=await hooks.watch();if(token!==epoch||!allowed())return;failureCount=0;
        if(phase==='recovering'&&!busy)state('watching');
        hooks.work?.(events);
        const fresh=events.filter(event=>!seen.has(event.id));for(const event of events)seen.add(event.id);
        while(seen.size>1000)seen.delete(seen.values().next().value);
        hooks.saveSeen?.([...seen]);
        const meaningful=fresh.filter(event=>event.kind!=='task'||!['running','queued'].includes(event.status));
        const report=baseline?meaningful:meaningful.filter(event=>event.priority>=3);
        if(report.length&&(!hooks.announceEvents||hooks.announceEvents())){const latest=report.slice(0,5);hooks.events?.(latest);notices.push(...latest.map(event=>[event.text,event.blocker,event.nextAction].filter(Boolean).join(' ')));notices=notices.slice(-5);if(!busy&&!finalText&&!interim)void announce();}baseline=true;
        if(hooks.initiative&&allowed()&&!busy&&!speaking&&!notices.length&&!finalText&&!interim&&Date.now()-lastActivity>=30000&&Date.now()-lastInitiativeCheck>=30000&&(!hooks.canInitiate||hooks.canInitiate())){
          lastInitiativeCheck=Date.now();const pending=new AbortController();initiativeController=pending;
          try{const result=await hooks.initiative(pending.signal);
            const current=()=>token===epoch&&allowed()&&!pending.signal.aborted&&!busy&&(!hooks.canInitiate||hooks.canInitiate());
            if(current()&&result?.reply){await hooks.proactiveReply?.(result.reply);if(current()){notices.push(result.reply);autonomousAudio=true;void announce().finally(()=>{autonomousAudio=false;});}}
          }finally{if(initiativeController===pending)initiativeController=null;}
        }
      }catch(error){if(token===epoch&&error.name!=='AbortError'){failureCount++;if(error.status===401||error.status===423){hooks.error?.(error.message);stop();}else if(failureCount===3){hooks.error?.(error.message);state('recovering');}}}
      finally{polling=false;}}
      if(allowed())pollTimer=later(()=>void poll(),failureCount>=3?30000:10000);
    }
    async function start(withMicrophone=false){
      activity();
      if(!hooks.valid())return;
      if(!enabled){enabled=true;epoch++;const initial=hooks.loadSeen?.();seen=new Set(Array.isArray(initial)?initial.filter(id=>typeof id==='string').slice(-1000):[]);baseline=false;ended=[];failureCount=0;}
      paused=false;microphone=withMicrophone||microphone;state(microphone?'requesting':'watching');clear(pollTimer);void poll();if(microphone)await listen();
    }
    function resume(){if(!enabled)return;paused=false;epoch++;ended=[];failureCount=0;void poll();microphone?void listen():state('watching');}
    function reply(text){if(!allowed()||!text)return;notices.push(text);if(!busy&&!finalText&&!interim)void announce();}
    function setMicrophone(value){
      microphone=!!value;ended=[];
      if(microphone){if(!enabled||paused)void start(true);else void listen();}
      else{release();finalText=interim='';if(allowed()&&!busy)state('watching');}
    }
    return Object.freeze({start,stop,pause,resume,snapshot,reply,setMicrophone,activity});
  }
  window.AideAssistantLive=Object.freeze({create});
})();
