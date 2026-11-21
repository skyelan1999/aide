(() => {
 'use strict';
 const outputRate=24000, scheduleAhead=0.35, maxQueuedSamples=90*outputRate;
 function create(hooks){
  let enabled=false,microphone=false,phase='off',epoch=0,stream=null,context=null,node=null,source=null,gain=null,ws=null,ping=0,muted=false,inputSerial=0;
  let nextTime=0,queuedSamples=0,ignoreAudio=false,responding=false,caption='',responseID='',itemID='';
  const players=new Map(),pendingAudio=[],receivedSamples=new Map(),discarded=new Set();
  let metadata={},inputRate=16000,phone=null,speaker=null;
  const snapshot=()=>({enabled,microphone,paused:false,phase,...metadata});
  const state=value=>{phase=value;hooks.state?.(snapshot());};
  const send=event=>{if(ws?.readyState===1){if(ws.bufferedAmount>256000){hooks.error('实时音频网络拥堵，通话已结束');stop();return;}ws.send(JSON.stringify(event));}};
  function clearAudio(){
   for(const player of players.keys()){
    player.onended=null;
    try{player.stop();}catch(_){}
    player.disconnect();
   }
   players.clear();pendingAudio.length=0;queuedSamples=0;nextTime=context?.currentTime||0;
  }
  function releaseInput(){inputSerial++;if(node){node.port.onmessage=null;node.disconnect();node=null;}source?.disconnect();source=null;gain?.disconnect();gain=null;if(!phone)stream?.getTracks().forEach(t=>t.stop());stream=null;}
  function stop(){epoch++;enabled=false;microphone=false;muted=false;clearInterval(ping);ping=0;releaseInput();clearAudio();const handset=phone;phone=null;speaker=null;handset?.close();if(ws){const old=ws;ws.onclose=ws.onmessage=ws.onerror=null;try{if(old.readyState===1)old.send(JSON.stringify({type:'aide.hangup'}));}catch(_){}try{old.close();}catch(_){}ws=null;}const old=context;context=null;if(old)void old.close().catch(()=>{});state('off');}
  function fatal(message){hooks.error(message);stop();}
  async function input(token){
   const ticket=++inputSerial;
   const acquired=phone?.stream||await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true,noiseSuppression:true,autoGainControl:true,...(hooks.device?.()?{deviceId:{exact:hooks.device()}}:{})}});
   if(token!==epoch||ticket!==inputSerial||!enabled||muted){if(!phone)acquired.getTracks().forEach(t=>t.stop());return;}
   stream=acquired;source=context.createMediaStreamSource(stream);node=new AudioWorkletNode(context,'aide-pcm-input',{processorOptions:{inputRate}});gain=context.createGain();gain.gain.value=0;
   node.port.onmessage=event=>{if(token!==epoch||muted||phase==='requesting'||!hooks.valid())return;let s='';for(const byte of new Uint8Array(event.data))s+=String.fromCharCode(byte);send({type:'input_audio_buffer.append',audio:btoa(s)});};
   source.connect(node);node.connect(gain);gain.connect(context.destination);
  }
  function scheduleAudio(){
   // Providers can deliver several seconds of PCM in a burst. Only schedule
   // the next part on the audio clock, keeping the rest in a bounded queue.
   const now=context.currentTime;
   while(pendingAudio.length&&players.size<24&&(players.size<2||nextTime-now<scheduleAhead)){
    const chunk=pendingAudio.shift();
    queuedSamples-=chunk.samples;
    const view=new DataView(chunk.bytes.buffer);
    const buffer=context.createBuffer(1,chunk.samples,outputRate),samples=buffer.getChannelData(0);
    for(let i=0;i<samples.length;i++)samples[i]=view.getInt16(i*2,true)/32768;
    const player=context.createBufferSource();
    player.buffer=buffer;player.connect(speaker||context.destination);
    const start=nextTime>now?nextTime:now+0.025;
    nextTime=start+buffer.duration;
    players.set(player,{item:chunk.item,offset:chunk.offset,samples:chunk.samples,start,end:nextTime});
    player.onended=()=>{
     players.delete(player);player.disconnect();
     if(!enabled||!context)return;
     scheduleAudio();
     if(!players.size&&!responding)state(muted?'watching':'listening');
    };
    player.start(start);
   }
  }
  function interruptAudio(){
   const now=context.currentTime;
   const next=[...players.values()].find(chunk=>chunk.end>now)||pendingAudio[0];
   if(next?.item){
    const played=next.start===undefined?0:Math.max(0,Math.floor((now-next.start)*outputRate));
    const audioEndMS=Math.floor((next.offset+Math.min(next.samples,played))*1000/outputRate);
    send({type:'conversation.item.truncate',item_id:next.item,audio_end_ms:audioEndMS});
   }
   ignoreAudio=true;
   if(responseID){
    discarded.add(responseID);
    if(discarded.size>128)discarded.delete(discarded.values().next().value);
   }
   clearAudio();
   if(responding)send({type:'response.cancel'});
   responding=false;caption='';
   if(enabled)state(muted?'watching':'listening');
  }
  function audio(base64,item){
   if(ignoreAudio||!context)return;
   const data=atob(base64);
   if(!data.length||data.length%2||data.length>192000){fatal('实时音频格式无效');return;}
   const samples=data.length/2;
   if(pendingAudio.length>=8192||queuedSamples+samples+Math.max(0,nextTime-context.currentTime)*outputRate>maxQueuedSamples){
    interruptAudio();
    if(enabled)hooks.error('本轮语音播放超出缓冲上限，可以继续通话');
    return;
   }
   const offset=receivedSamples.get(item)||0;
   receivedSamples.set(item,offset+samples);
   pendingAudio.push({bytes:Uint8Array.from(data,c=>c.charCodeAt(0)),samples,offset,item});
   queuedSamples+=samples;
   scheduleAudio();
   if(phase!=='speaking')state('speaking');
  }
  async function start(){
   if(enabled)return;if(!hooks.valid())return;
   const token=++epoch;metadata={};inputRate=16000;enabled=true;microphone=true;muted=false;
   nextTime=0;ignoreAudio=false;responding=false;caption='';responseID='';itemID='';receivedSamples.clear();discarded.clear();
   state('requesting');hooks.beforeStart?.();
   try{
    const Ctor=window.AudioContext||window.webkitAudioContext;context=new Ctor();const resumed=context.resume();
    const [,config,route]=await Promise.all([resumed,hooks.preflight?.(),hooks.voip?.()]);if(token!==epoch)return;
    inputRate=config?.inputRate===24000?24000:16000;
    metadata={provider:config?.provider||'',model:config?.model||'',voice:config?.voice||''};
    await context.audioWorklet.addModule('/assistant-pcm-worklet.js?v=2');
    if(token!==epoch)return;
    if(route?.assistant){
     metadata={...metadata,statusMessage:'正在呼叫局域网话机'};state('requesting');
     phone=window.AideVoIP.open({mode:'assistant',token:hooks.token(),permission:{session:hooks.session()},context,
      onStatus:message=>{metadata={...metadata,statusMessage:message};state('requesting');},
      onEnd:message=>{if(token===epoch&&enabled){if(message)hooks.error(message);stop();}}});
     await phone.ready;
     if(token!==epoch||!enabled)return;
     speaker=phone.speaker;
     metadata={...metadata,statusMessage:''};state('requesting');
    }
    await input(token);if(token!==epoch||!enabled)return;
    const url=new URL(`/api/sessions/${hooks.session()}/assistant-call/socket`,location.href);url.protocol=location.protocol==='https:'?'wss:':'ws:';
    ws=new WebSocket(url,['aide-realtime','aide-auth.'+hooks.token()]);
    ws.onmessage=event=>{if(token!==epoch||!enabled)return;try{
     const e=JSON.parse(event.data);
     if(e.response_id&&discarded.has(e.response_id))return;
     if(e.type==='response.done'&&discarded.has(e.response?.id))return;
     if(e.type==='aide.error'){fatal(e.message);return;}
     if(e.type==='aide.approval'){hooks.approval?.(e);return;}
     if(e.type==='aide.ready'){metadata={provider:e.provider||metadata.provider,model:e.model||metadata.model,voice:e.voice||metadata.voice};state('listening');hooks.ready?.(e);return;}
     if(e.type==='input_audio_buffer.speech_started')interruptAudio();
     else if(e.type==='input_audio_buffer.speech_stopped')state('thinking');
     else if(e.type==='response.created'){responding=true;ignoreAudio=false;caption='';responseID=e.response?.id||'';itemID='';receivedSamples.clear();state('thinking');}
     else if(e.type==='response.audio.delta'){itemID=e.item_id||itemID;audio(e.delta,itemID);}
     else if(e.type==='response.audio_transcript.delta'){caption+=e.delta;hooks.transcript?.('assistant',caption);}
     else if(e.type==='conversation.item.input_audio_transcription.completed')hooks.transcript?.('user',e.transcript);
     else if(e.type==='response.done'){responding=false;if(!players.size)state(muted?'watching':'listening');hooks.turn?.();}
    }catch(_){fatal('实时音频数据异常，通话已结束');}};
    ws.onerror=()=>fatal('原生实时连接失败，请检查所选语音模型、密钥和网络');
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
