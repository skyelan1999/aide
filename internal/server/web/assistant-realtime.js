(() => {
 'use strict';
 function create(hooks){
  let enabled=false,microphone=false,phase='off',epoch=0,stream=null,context=null,node=null,source=null,gain=null,ws=null,ping=0,muted=false,inputSerial=0;
  let nextTime=0,players=new Set(),ignoreAudio=false,responding=false,caption='',responseID='',itemID='',firstAudioTime=0,discarded=new Set();
  const snapshot=()=>({enabled,microphone,paused:false,phase});
  const state=value=>{phase=value;hooks.state?.(snapshot());};
  const send=event=>{if(ws?.readyState===1){if(ws.bufferedAmount>256000){hooks.error('实时音频网络拥堵，通话已结束');stop();return;}ws.send(JSON.stringify(event));}};
  function clearAudio(){for(const player of players){player.onended=null;try{player.stop();}catch(_){}}players.clear();nextTime=context?.currentTime||0;}
  function releaseInput(){inputSerial++;if(node){node.port.onmessage=null;node.disconnect();node=null;}source?.disconnect();source=null;gain?.disconnect();gain=null;stream?.getTracks().forEach(t=>t.stop());stream=null;}
  function stop(){epoch++;enabled=false;microphone=false;muted=false;clearInterval(ping);ping=0;releaseInput();clearAudio();if(ws){ws.onclose=ws.onmessage=ws.onerror=null;ws.close();ws=null;}const old=context;context=null;if(old)void old.close().catch(()=>{});state('off');}
  function fatal(message){hooks.error(message);stop();}
  async function input(token){
   const ticket=++inputSerial;
   const acquired=await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true,noiseSuppression:true,autoGainControl:true,...(hooks.device?.()?{deviceId:{exact:hooks.device()}}:{})}});
   if(token!==epoch||ticket!==inputSerial||!enabled||muted){acquired.getTracks().forEach(t=>t.stop());return;}
   stream=acquired;source=context.createMediaStreamSource(stream);node=new AudioWorkletNode(context,'aide-pcm-input');gain=context.createGain();gain.gain.value=0;
   node.port.onmessage=event=>{if(token!==epoch||muted||phase==='requesting'||!hooks.valid())return;let s='';for(const byte of new Uint8Array(event.data))s+=String.fromCharCode(byte);send({type:'input_audio_buffer.append',audio:btoa(s)});};
   source.connect(node);node.connect(gain);gain.connect(context.destination);
  }
  function audio(base64){
   if(ignoreAudio||!context)return;
   const data=atob(base64);if(data.length%2||data.length>192000){fatal('实时音频格式无效');return;}
   const bytes=Uint8Array.from(data,c=>c.charCodeAt(0)),view=new DataView(bytes.buffer),buffer=context.createBuffer(1,bytes.length/2,24000),samples=buffer.getChannelData(0);
   for(let i=0;i<samples.length;i++)samples[i]=view.getInt16(i*2,true)/32768;
   if(nextTime-context.currentTime>12){fatal('实时音频播放积压，通话已结束');return;}
   const player=context.createBufferSource();player.buffer=buffer;player.connect(context.destination);players.add(player);
   player.onended=()=>{players.delete(player);if(!players.size&&!responding&&enabled)state(muted?'watching':'listening');};
   nextTime=Math.max(context.currentTime+0.025,nextTime);if(!firstAudioTime)firstAudioTime=nextTime;player.start(nextTime);nextTime+=buffer.duration;state('speaking');
  }
  async function start(){
   if(enabled)return;if(!hooks.valid())return;
   const token=++epoch;enabled=true;microphone=true;muted=false;state('requesting');hooks.beforeStart?.();
   try{
    const Ctor=window.AudioContext||window.webkitAudioContext;context=new Ctor();const resumed=context.resume();
    await Promise.all([resumed,hooks.preflight?.()]);if(token!==epoch)return;await context.audioWorklet.addModule('/assistant-pcm-worklet.js?v=1');
    if(token!==epoch)return;
    await input(token);if(token!==epoch||!enabled)return;
    const url=new URL(`/api/sessions/${hooks.session()}/assistant-call/socket`,location.href);url.protocol=location.protocol==='https:'?'wss:':'ws:';
    ws=new WebSocket(url,['aide-realtime','aide-auth.'+hooks.token()]);
    ws.onmessage=event=>{if(token!==epoch||!enabled)return;try{
     const e=JSON.parse(event.data);
     if(e.response_id&&discarded.has(e.response_id))return;
     if(e.type==='response.done'&&discarded.has(e.response?.id))return;
     if(e.type==='aide.error'){fatal(e.message);return;}
     if(e.type==='aide.ready'){state('listening');hooks.ready?.(e);return;}
     if(e.type==='input_audio_buffer.speech_started'){ignoreAudio=true;if(responseID){discarded.add(responseID);if(discarded.size>128)discarded.delete(discarded.values().next().value);}
      if(itemID&&firstAudioTime)send({type:'conversation.item.truncate',item_id:itemID,audio_end_ms:Math.max(0,Math.floor((Math.min(context.currentTime,nextTime)-firstAudioTime)*1000))});
      clearAudio();if(responding)send({type:'response.cancel'});responding=false;caption='';state('listening');}
     else if(e.type==='input_audio_buffer.speech_stopped')state('thinking');
     else if(e.type==='response.created'){responding=true;ignoreAudio=false;caption='';responseID=e.response?.id||'';itemID='';firstAudioTime=0;state('thinking');}
     else if(e.type==='response.audio.delta'){itemID=e.item_id||itemID;audio(e.delta);}
     else if(e.type==='response.audio_transcript.delta'){caption+=e.delta;hooks.transcript?.('assistant',caption);}
     else if(e.type==='conversation.item.input_audio_transcription.completed')hooks.transcript?.('user',e.transcript);
     else if(e.type==='response.done'){responding=false;if(!players.size)state(muted?'watching':'listening');hooks.turn?.();}
    }catch(_){fatal('实时音频数据异常，通话已结束');}};
    ws.onerror=()=>fatal('原生实时连接失败，请检查千问业务空间和模型开通状态');
    ws.onclose=()=>{if(token===epoch&&enabled)fatal('原生实时连接已结束，请重新拨通');};
    ping=setInterval(()=>{if(!hooks.valid()){stop();return;}send({type:'aide.ping'});},10000);
   }catch(error){if(token===epoch)fatal(error.message||'无法开启原生实时音频');}
  }
  async function setMicrophone(value){
   if(!enabled)return;muted=!value;microphone=!!value;
   if(muted){releaseInput();send({type:'input_audio_buffer.clear'});state(players.size?'speaking':'watching');}
   else{const token=epoch;try{await input(token);if(token===epoch&&enabled)state(players.size?'speaking':'listening');}catch(error){if(token===epoch)fatal(error.message);}}
  }
  return Object.freeze({start,stop,setMicrophone,snapshot});
 }
 window.AideAssistantRealtime=Object.freeze({create});
})();
