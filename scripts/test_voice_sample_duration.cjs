'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function recorderSource(){
 const app=fs.readFileSync('internal/server/web/app.js','utf8');
 const start=app.indexOf('function renderVoiceCloneControl() {'),end=app.indexOf("\ncontrolRenderers['voice-clone']",start);
 assert.ok(start>=0&&end>start,'voice recorder product function found');return app.slice(start,end);
}
class Element{
 constructor(tag,cls='',text=''){this.tag=tag;this.cls=cls;this.textContent=text;this.children=[];this.style={};this.disabled=false;this.checked=false;}
 append(...items){this.children.push(...items);}
 set innerHTML(_){this.children=[];}
}
function fixture(permissionDelay=0){
 let clock=1000,stoppedTracks=0;const recordings=[],uploads=[];
 class Recorder{
  constructor(){this.state='inactive';this.mimeType='audio/webm';recordings.push(this);}
  start(){this.state='recording';this.started=clock;}
  stop(){this.state='inactive';this.stopped=clock;}
  async deliver(){this.ondataavailable({data:new Blob(['owned-audio'],{type:this.mimeType})});await this.onstop();}
 }
 const context={state:{config:{},token:'owned-fixture-token'},el:(...args)=>new Element(...args),t:value=>value,action:fn=>fn,toast:()=>{},refreshConfig:async()=>{},
  api:async path=>{assert.equal(path,'/voice-samples');return {samples:[]};},MediaRecorder:Recorder,Blob,FormData,encodeURIComponent,performance:{now:()=>clock},
  navigator:{mediaDevices:{getUserMedia:async()=>{clock+=permissionDelay;return {getTracks:()=>[{stop(){stoppedTracks++;}}]};}}},
  fetch:async(url,options)=>{uploads.push({url,options});return {ok:true,json:async()=>({id:'owned-sample'})};}};
 vm.createContext(context);vm.runInContext(recorderSource(),context);const wrap=vm.runInContext('renderVoiceCloneControl()',context);
 const all=[];function walk(node){all.push(node);for(const child of node.children||[])walk(child);}walk(wrap);
 const start=all.find(node=>node.textContent==='开始录音'),stop=all.find(node=>node.textContent==='停止并上传');
 const consent=all.find(node=>node.tag==='input'&&node.type==='checkbox');consent.checked=true;
 return {start,stop,recordings,uploads,get stoppedTracks(){return stoppedTracks;},setClock:value=>{clock=value;}};
}
async function run(){
 for(const [elapsed,expected,permissionDelay] of [[30000,30,0],[120000,120,20000],[450,1,0],[12400,12,0]]){
  const f=fixture(permissionDelay);await f.start.onclick();const rec=f.recordings[0];
  assert.equal(f.start.disabled,true);assert.equal(f.stop.disabled,false);f.setClock(rec.started+elapsed);f.stop.onclick();
  f.setClock(rec.stopped+3500);await rec.deliver();
  assert.equal(f.uploads.length,1);const upload=f.uploads[0],url=new URL(upload.url,'http://owned.invalid');
  assert.equal(Number(url.searchParams.get('durationSec')),expected,'duration must measure recorder start to requested stop, excluding permission/encoding wait');
  assert.equal(upload.options.body.type,'audio/webm');assert.equal(await upload.options.body.text(),'owned-audio');
  assert.equal(f.stoppedTracks,1);assert.equal(f.start.disabled,false);assert.equal(f.stop.disabled,true);
 }
 const spontaneous=fixture();await spontaneous.start.onclick();const rec=spontaneous.recordings[0];
 spontaneous.setClock(rec.started+8000);rec.state='inactive';await rec.deliver();
 assert.equal(Number(new URL(spontaneous.uploads[0].url,'http://owned.invalid').searchParams.get('durationSec')),8,'spontaneous recorder stop uses elapsed clock');
 console.log('PASS actual elapsed sample duration, permission/encoding exclusion, short/fractional recording, MIME/audio and stop cleanup');
}
module.exports={recorderSource};
if(require.main===module)run().catch(error=>{console.error(error);process.exitCode=1;});
