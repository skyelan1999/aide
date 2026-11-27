'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/experience.js','utf8');
(async()=>{
for(const mode of ['normal','balanced','minimal','hidden','reduced']){
 const root={dataset:{motion:mode==='reduced'?'reduced':'auto',motionProfile:['balanced','minimal'].includes(mode)?mode:'full'},toggleAttribute(){}};
 const animated=mode==='normal'||mode==='balanced';
 let added=0,removed=0,idLookup=()=>null,lastLayer=null,pressureCallback=null;
 const frames=[];
 const segmentClasses=new Set(),segmentStyles=new Map(),segmentObservers=[];
 let selectedSegment={offsetLeft:141,offsetWidth:67};
 const segment={isConnected:true,closest:()=>null,getClientRects:()=>[1],
  querySelector:selector=>selector==='button[aria-pressed="true"]'?selectedSegment:null,
  classList:{contains:name=>segmentClasses.has(name),add:(...names)=>names.forEach(name=>segmentClasses.add(name)),remove:(...names)=>names.forEach(name=>segmentClasses.delete(name))},
  style:{setProperty:(name,value)=>segmentStyles.set(name,value),removeProperty:name=>segmentStyles.delete(name)}};
 const paths=[];
 const animations=[],events={};
 const animate=()=>{let resolve,reject;const finished=new Promise((yes,no)=>{resolve=yes;reject=no;});const a={finished,finish:resolve,cancel(){reject(new Error('cancelled'));}};animations.push(a);return a;};
 const flush=async()=>{for(let i=0;i<4;i++)await Promise.resolve();};
 const body={classList:{contains:()=>false},animate:null,append(node){added++;lastLayer=node}};
 const svgNode=(_namespace,tag)=>{const node={attrs:{},classList:{add(){}},style:{},setAttribute(name,value){this.attrs[name]=value},append(){},remove(){removed++},getTotalLength(){return 200},animate};if(tag==='path')paths.push(node);return node};
 const context={document:{documentElement:root,body,hidden:mode==='hidden',getElementById:id=>idLookup(id),querySelector:()=>null,createElementNS:svgNode,addEventListener(){},removeEventListener(){}},location:{pathname:'/',hash:''},sessionStorage:{getItem:()=> '1'},MutationObserver:class{observe(){}disconnect(){}},getComputedStyle:()=>({gridTemplateColumns:'100px 100px',display:'block',visibility:'visible'}),innerWidth:1440,innerHeight:900,performance:{now:()=>1000},Event:class{},setTimeout,clearTimeout,cancelAnimationFrame(){},requestAnimationFrame:fn=>(frames.push(fn),frames.length)};
 context.window=context;context.addEventListener=(name,fn)=>{events[name]=fn;};context.dispatchEvent=()=>{};context.matchMedia=()=>({matches:false,addEventListener(){}});
 context.PerformanceObserver=class{static supportedEntryTypes=['longtask'];constructor(fn){pressureCallback=fn;}observe(){}disconnect(){}};
 context.document.querySelector=selector=>selector==='.panel-switch'?segment:null;
 context.MutationObserver=class{constructor(fn){this.fn=fn;}observe(node,options){if(node===segment)segmentObservers.push({fn:this.fn,options});}disconnect(){}};
 vm.createContext(context);vm.runInContext(source,context,{filename:'experience.js'});
 assert.equal(segmentStyles.get('--motion-segment-x'),'141px','pressed selection gets an indicator');
 selectedSegment={offsetLeft:70,offsetWidth:34};events.resize();
 while(frames.length)frames.shift()();
 assert.equal(segmentStyles.get('--motion-segment-x'),'70px','resize remeasures the selected indicator');
 assert.equal(segmentStyles.get('--motion-segment-width'),'34px');
 assert(segmentObservers[0].options.attributeFilter.includes('aria-pressed'));
 selectedSegment=null;segmentObservers[0].fn();
 while(frames.length)frames.shift()();
 assert.equal(segmentClasses.has('motion-segment-ready'),false,'deselection clears the stale indicator');
 assert.equal(segmentStyles.size,0,'deselection removes stale geometry');
 const api=context.aideExperience;
 assert(api,mode+': complete script creates the API');assert.equal(root.dataset.motionLayer,'ready');
 for(const name of ['leave','settle','reopen','changePanels','revealContent','beginSend'])assert.equal(typeof api[name],'function',name);
 assert.equal(api.replay,undefined,'removed replay API stays removed');
 const markerFrames=[];
 const marker={...segment,getBoundingClientRect:()=>({left:85,top:16,width:32}),animate(keyframes,options){markerFrames.push(keyframes);return animate(keyframes,options);}};
 api.moveSettingsIndicator(marker,{left:45,top:16,width:64});
 assert.equal(markerFrames.length,animated?1:0,'underline motion follows the effective system/device profile');
 if(animated)assert.equal(markerFrames[0][0].transform,'translateX(-40px) scaleX(2)');
 api.moveSettingsIndicator(marker,{left:45,top:58,width:64});
 assert.equal(markerFrames.length,animated?1:0,'moving to another tab row never sweeps across unrelated text');
 api.settle(marker);

 let closed=0,changed=0;
 const finish=api.leave(null,()=>closed++);finish();api.settle(null);api.reopen(null);api.changePanels(()=>changed++);
 assert.equal(closed,1);assert.equal(changed,1,'panel mutation is immediate');
 const classes=new Set(),composer={isConnected:true,closest:()=>null,querySelector:()=>null,animate,getClientRects:()=>[1],getBoundingClientRect:()=>({left:300,right:1100,top:700})};
 let buttonHidden=false;
 const button={isConnected:true,closest:()=>composer,getBoundingClientRect:()=>buttonHidden?{left:0,top:0,width:0,height:0}:{left:1050,top:770,width:30,height:30},animate(){},classList:{add:name=>classes.add(name),remove:name=>classes.delete(name)}};
 const send=api.beginSend(button);
 assert.equal(classes.has('send-beaming'),animated,'send motion follows visibility and reduced-motion preference');
 assert.equal(added,animated?1:0,'only visible motion creates a light route');
 send.land(null);
 while(frames.length)frames.shift()();
 assert.equal(classes.has('send-beaming'),false,'missing or failed message leaves no active glow');
 assert.equal(removed,animated?1:0,'failed message removes the light route');
 let stripTop=650;
 const strip={isConnected:true,closest:()=>null,getClientRects:()=>[1],getBoundingClientRect:()=>({left:300,top:stripTop})};
 const stopButton={isConnected:true,closest:selector=>selector==='.composer-run-controls'?strip:null,getClientRects:()=>[1],getBoundingClientRect:()=>({left:1010,top:660,width:60,height:28}),animate(){}};
 idLookup=id=>id==='send'?button:null;
 const stop=api.beginStop(stopButton,composer);
 assert.equal(added,animated?2:0,'visible stop control starts the return route');
 assert.equal(classes.has('send-returning'),animated,'returning beam hides the send arrow until it arrives');
 stop.cancel();
 assert.equal(removed,animated?2:0,'cancelled stop removes the return route');
 assert.equal(classes.has('send-returning'),false,'cancelled stop restores the send arrow');
 if(animated){
  idLookup=id=>id==='run-controls'?strip:id==='stop-active-run'?stopButton:null;
  const start=paths.length,connected=api.beginSend(button);
  assert.equal(paths[start+1].attrs.d,undefined,'send bar waits for the actual run strip');
  connected.land(null);
  assert.equal(paths[start+1].attrs.d,undefined,'send motion waits for the next layout frame');
  frames.shift()();
  assert.match(paths[start].attrs.d,/^M 324 651 H /,'outbound rail uses the visible run strip top');
  assert.match(paths[start+1].attrs.d,/ 651 /,'outbound beam joins the same measured edge');
  stripTop=675;frames.shift()();
  assert.equal(lastLayer.style.transform,'translate3d(0px,25px,0)','outbound route follows a run strip that shifts during layout');
  events.resize();await flush();
  idLookup=id=>id==='send'?button:null;
  const before=removed,returnMotion=api.beginStop(stopButton,composer);
  returnMotion.land();
  for(const a of [...animations])a.finish();await flush();
  assert.equal(classes.has('send-returning'),true,'arrow remains hidden throughout the return leg');
  for(const a of [...animations])a.finish();await flush();
  assert.equal(classes.has('send-returning'),false,'completed return restores the arrow');
  assert.equal(removed,before+1,'completed layered animation removes its overlay once');
  api.beginStop(stopButton,composer);events.resize();await flush();
  assert.equal(classes.has('send-returning'),false,'resize cancels all light layers and restores the arrow');
 }
 context.aideExperience=null;vm.runInContext(source,context);assert.equal(typeof context.aideExperience.leave,'function','null API initializes');context.aideExperience=api;
 api.extension='retained';vm.runInContext(source,context);assert.equal(context.aideExperience,api,'preserve existing API identity');assert.equal(api.extension,'retained');
 if(mode==='normal'){
  pressureCallback({getEntries:()=>[{duration:90,startTime:990}]});assert.equal(root.dataset.motionProfile,'full','one isolated long task does not downgrade motion');
  pressureCallback({getEntries:()=>[{duration:90,startTime:991}]});assert.equal(root.dataset.motionProfile,'balanced','repeated long tasks reduce decorative work');
  pressureCallback({getEntries:()=>[{duration:100,startTime:992},{duration:100,startTime:993},{duration:100,startTime:994}]});assert.equal(root.dataset.motionProfile,'minimal','sustained pressure disables decorative motion');
 }
}
assert(!source.includes('body.animate([{gridTemplateColumns'),'panel tracks never animate grid layout');
console.log('PASS complete experience runtime: full, balanced, minimal, hidden, reduced; public callbacks and API identity');
})().catch(error=>{console.error(error);process.exitCode=1;});
