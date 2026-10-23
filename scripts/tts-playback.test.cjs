'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
const start=source.indexOf('const ttsPlayer ='),end=source.indexOf('function ttsWantsEdge(',start);
const play=source.slice(source.indexOf('function ttsPlayOne('),source.indexOf('// 朗读入口：'));
let created=0,blocked=false,retry=null,phases=[],revoked=[],intervals=new Map(),serial=0;
class Audio {
  constructor(){created++;this.paused=true;this.currentTime=0;this.ended=false;}
  play(){
    if(blocked)return Promise.reject(Object.assign(Error('gesture needed'),{name:'NotAllowedError'}));
    this.paused=false;this.onplaying?.();return Promise.resolve();
  }
  pause(){this.paused=true;this.onpause?.();}
  removeAttribute(name){if(name==='src')this.src='';}
}
const context={Audio,Uint8Array,DataView,Blob,Date,Promise,window:{},document:{body:{append(){}},getElementById:()=>null},state:{config:{ttsProvider:'qwen',ttsRate:1.2}},t:x=>x,URL:{createObjectURL:()=>`blob:prime-${++serial}`,revokeObjectURL:u=>revoked.push(u)},setInterval:fn=>{const id=++serial;intervals.set(id,fn);return id;},clearInterval:id=>intervals.delete(id)};
vm.createContext(context);
// Keep the production player, gesture unlock and playback lifecycle. UI rendering
// is replaced so the test can assert transitions without a synthetic DOM layout.
vm.runInContext(source.slice(start,end)+play+'\nthis.player=ttsPlayer;',context);
context.ttsPlaybackStatus=(phase,detail,callback)=>{phases.push(phase);retry=callback;};
const tick=async()=>{for(let i=0;i<5;i++)await Promise.resolve();};
(async()=>{
 context.ttsUnlockAudio();await tick();assert.equal(created,1);assert(context.player._unlocked);
 const first=context.ttsPlayOne('blob:first','qwen');await tick();const a=context.player._audio;
 assert.equal(created,1,'async synthesis must reuse the gesture-activated element');assert.equal(a.playbackRate,1.2);assert.equal(phases.at(-1),'playing');
 a.currentTime=1;a.onended();await first;assert(revoked.includes('blob:first'));assert.equal(intervals.size,0);
 context.window.aideAssistantCall={active:()=>true};const callAudio=context.ttsPlayOne('blob:call','qwen');await tick();assert.equal(a.playbackRate,1,'calls do not inherit accelerated narration');a.onended();await callAudio;assert.equal(context.state.config.ttsRate,1.2);delete context.window.aideAssistantCall;
 blocked=true;let settled=false;const second=context.ttsPlayOne('blob:blocked','qwen').then(()=>{settled=true;});await tick();
 assert.equal(phases.at(-1),'blocked');assert.equal(settled,false);assert.equal(typeof retry,'function');
 const blockedRetry=retry;blocked=false;blockedRetry();await tick();assert.equal(phases.at(-1),'playing');assert.equal(a.src,'blob:blocked','gesture retry must reuse audio, not bill another synthesis');
 context.player.halt();await second;assert.equal(settled,true);assert.equal(intervals.size,0);assert.equal(phases.at(-1),'idle');
 const calls=created;blockedRetry();await tick();assert.equal(a.src,'','stale retry after cancellation cannot restart playback');assert.equal(created,calls);
 context.player.cancelled=false;a.currentTime=0;const third=context.ttsPlayOne('blob:bad','qwen');a.onerror();await assert.rejects(third,/解码|播放/);assert.equal(intervals.size,0);
 console.log('PASS gesture activation, element reuse, blocked retry without synthesis, interruption cleanup and decode failure');
 // Exercise the actual text-response route, including selectSession cancelling
 // previous audio. A new reply must be queued AFTER that cancellation.
 const routeStart=source.indexOf('// 小秘文字会话不经过 aide');
 const routeEnd=source.indexOf('// 按 action 分流提示',routeStart);
 const route=source.slice(routeStart,routeEnd);
 for(const live of [false,true]){
  const order=[];const target={id:'assistant'};
  const routeContext={resp:{reply:'Test reply'},target,submittedScope:'scope',submittedDraft:{},recoveryScope:()=> 'scope',clearSubmittedDraft:async()=>{},state:{session:target,attachments:[]},chatContent:()=>'',renderAttachments(){},$:()=>({value:''}),window:{aideAssistantLive:{snapshot:()=>({enabled:live}),reply:text=>order.push('queued:'+text)}},selectSession:async()=>{order.push('refresh/cancel');},speakReply:text=>order.push('speak:'+text)};
  vm.createContext(routeContext);await vm.runInContext('(async()=>{'+route+'})()',routeContext);
  assert.deepEqual(order,['refresh/cancel',(live?'queued:':'speak:')+'Test reply']);
 }
 console.log('PASS typed assistant replies are spoken after refresh, both with and without companion mode');
 let replies=0;
 const voiceSettings={window:{},state:{config:{voiceReplyEnabled:false}},document:{visibilityState:'visible'},stripMarkdownForSpeech:text=>text,ttsSpeak:async()=>{replies++;}};
 vm.createContext(voiceSettings);
 const speakHook=source.split('\n').find(line=>line.trim().startsWith('speak:text=>'));
 const speakFromSettings=vm.runInContext('({'+speakHook+'}).speak',voiceSettings);
 await speakFromSettings('Silent setting');assert.equal(replies,0);
 voiceSettings.state.config.voiceReplyEnabled=true;await speakFromSettings('Enabled setting');assert.equal(replies,1);
 voiceSettings.state.config.voiceReplyEnabled=false;
 voiceSettings.window.aideAssistantCall={active:()=>true};await speakFromSettings('Explicit call');assert.equal(replies,1,'native call owns audio; legacy TTS must stay quiet');
 assert.equal(voiceSettings.state.config.voiceReplyEnabled,false);
 voiceSettings.document.visibilityState='hidden';await speakFromSettings('Hidden tab');assert.equal(replies,1);
 voiceSettings.document.visibilityState='visible';voiceSettings.window.aideAssistantCall={active:()=>false};await speakFromSettings('After hangup');assert.equal(replies,1,'saved silence returns after hangup');
 let elements=0,notice=null,retries=0;
 const statusContext={state:{},t:text=>text,el:(tag,cls,text)=>{elements++;return{tag,text,append(){},setAttribute(){},remove(){notice=null;}};},document:{getElementById:()=>notice,querySelector:()=>null,body:{append:node=>{notice=node;}}}};
 vm.createContext(statusContext);
 vm.runInContext(source.slice(source.indexOf('function ttsPlaybackStatus('),source.indexOf('function ttsWantsEdge(')),statusContext);
 for(const phase of ['idle','synthesizing','playing','complete'])statusContext.ttsPlaybackStatus(phase);
 assert.equal(elements,0,'ordinary playback must not create a permanent status bar');
 statusContext.ttsPlaybackStatus('blocked','Permission needed',()=>retries++);assert(notice);assert.equal(notice.id,'tts-playback-notice');
 statusContext.ttsPlaybackStatus('idle');assert.equal(notice,null,'cancel removes the temporary permission prompt');
 console.log('PASS saved voice setting is authoritative; normal playback creates no toolbar; blocked prompt is temporary');
})().catch(error=>{console.error(error);process.exitCode=1;});
