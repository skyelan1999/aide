const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const elements=new Map(),el=id=>{if(!elements.has(id))elements.set(id,{textContent:'A',value:'A',replaceChildren(){this.textContent=''}});return elements.get(id)};
function context(saved,workspace='B'){
 const c=vm.createContext({$:el,params:new URLSearchParams(),motionReduced:()=>false,clearEvidencePath(){},locked:false,loading:false,stopLive(){},graphGeneration:0,codeView:false,AbortController,syncCodeView(){},say(){},api:async()=>({workspace}),recoveryInput:0,recoveryScopeLoaded:'A',pendingRecoveredView:null,recoveryLoading:false,window:{AideContinuity:{readTab:async()=>saved}},graph:{workspace:'A'},aiAbort:{abort(){c.aborted=true}},resetDocumentBatch(){c.reset=true},beginGraphTransition(){},applyLiveUpdate(update){c.graph=update},restoreSkyView(value){c.restored=value},liveCursor:'',knowledgeTimeline:{sync(){}},liveFailures:0,redraw(){},scheduleLive(){}});
 vm.runInContext(source.slice(source.indexOf('function defaultSkyView()'),source.indexOf('function restoreSkyView(')),c);
 vm.runInContext(source.slice(source.indexOf('async function load(){'),source.indexOf('function syncMotion()')),c);return c;
}
(async()=>{
 let c=context(null);await vm.runInContext('load()',c);assert.equal(c.restored.query,'');assert.equal(c.restored.retrievalMode,'nodes');assert.equal(c.restored.region,'');assert.equal(c.restored.camera.zoom,1);assert.equal(c.restored.infrared,false);assert.equal(c.aborted,true);assert.equal(c.reset,true);assert.equal(el('question').value,'');
 c=context({version:1,query:'B saved',codeView:false});await vm.runInContext('load()',c);assert.equal(c.restored.query,'B saved');
 c=context(null,'A');await vm.runInContext('load()',c);assert.equal(c.restored,undefined,'same workspace must retain live edits');
 c=context(null);c.api=async()=>{c.recoveryInput++;return {workspace:'B'}};c.window.AideContinuity.readTab=async()=>{c.recoveryInput++;return null};await vm.runInContext('load()',c);assert.equal(c.restored,undefined,'late recovery must not overwrite user input');
 console.log('Star map workspace defaults, saved scene, same scope and input race PASS (source VM)');
})().catch(e=>{console.error(e);process.exitCode=1});

// Execute the actual camera snapshot/restore functions, including transition
// destinations and wheel targets. Numeric proof is separate from Safari proof.
const widgets=new Map(),widget=id=>{if(!widgets.has(id))widgets.set(id,{value:'nodes',options:[{value:'all'},{value:'nodes'}],scrollTop:0,setAttribute(){}});return widgets.get(id)};
const searchPanel={scrollTop:81};widget('detail').scrollTop=131;widget('detail-text').scrollTop=870;
let snapshot;
const sky=vm.createContext({clearTimeout,locked:false,loading:false,recoveryLoading:false,recoveryTimer:0,recoveryWarning:false,
 window:{AideContinuity:{enabled:()=>true}},AideContinuity:{writeTab:async(scope,kind,id,value)=>{snapshot=structuredClone(value)}},
 graph:{workspace:'W'},cameraTween:{to:{yaw:1.2,pitch:-.25,zoom:1.7}},wheelZoom:{target:2.1},yaw:0,pitch:0,zoom:1,
 codeView:false,region:'workspace',edgeMode:'all',callDepth:'all',focusCode:false,skyTime:170,driftTime:220,
 document:{querySelector:()=>searchPanel},$:widget,cosmicScene:{group:{key:'deep'}},cosmicPath:()=>[{key:'root'},{key:'deep'}],
 selected:null,animate:false,infrared:{enabled:true,set(v){this.enabled=v}},say(){},
 currentSources:()=>[{region:'workspace'}],search(){},nodeByID:new Map(),invalidateView(){},refreshView(){},ensureCosmicView(){},
 cosmos:{find:k=>k==='root'||k==='deep'},enterCosmic(k){sky.entered=k},interruptCamera(){sky.cameraTween=null;sky.wheelZoom=null},
 clamp:(v,a,b)=>Math.max(a,Math.min(b,v)),skyFloat:{x:0,y:0},motionReduced:()=>false,syncMotion(){},syncCodeView(){},syncGraphChrome(){},syncCosmicNav(){},redraw(){}});
vm.runInContext(source.slice(source.indexOf('function persistSkyView(){'),source.indexOf('function scheduleSkyRecovery()')),sky);
vm.runInContext(source.slice(source.indexOf('function restoreSkyView(value){'),source.indexOf("addEventListener('scroll'")),sky);
vm.runInContext('persistSkyView()',sky);
assert.deepEqual(snapshot.camera,{yaw:1.2,pitch:-.25,zoom:2.1});
assert.equal(snapshot.scroll.detailText,870,'save the independently scrolling node text, not just its outer panel');
widget('detail-text').scrollTop=0;
sky.saved=snapshot;vm.runInContext('restoreSkyView(saved)',sky);
assert.equal(sky.yaw,1.2);assert.equal(sky.pitch,-.25);assert.equal(sky.zoom,2.1);
assert.equal(sky.skyTime,170);assert.equal(sky.driftTime,220);assert.equal(sky.entered,'deep');
assert.equal(searchPanel.scrollTop,81);assert.equal(widget('detail').scrollTop,131);assert.equal(sky.infrared.enabled,true);
assert.equal(widget('detail-text').scrollTop,870,'restore nonzero node text scroll after selection');
const legacy=structuredClone(snapshot);delete legacy.scroll.detailText;sky.legacy=legacy;
vm.runInContext('restoreSkyView(legacy)',sky);assert.equal(widget('detail-text').scrollTop,0,'older scene snapshots remain compatible');
sky.cosmos.find=k=>k==='root';vm.runInContext('restoreSkyView(saved)',sky);assert.equal(sky.entered,'root');
console.log('Star map tab-scoped snapshot, camera/wheel targets, motion phase, panel scroll and ancestor fallback PASS (source VM)');

const retry=vm.createContext({liveTimer:1,liveReady:()=>true,scheduleLive(){},liveAbort:null,graph:{},recoveryScopeLoaded:'',load(){retry.loads=(retry.loads||0)+1}});
vm.runInContext(source.slice(source.indexOf('async function pollLive(){'),source.indexOf('const retrievalMode=')),retry);
(async()=>{
 await vm.runInContext('pollLive()',retry);assert.equal(retry.loads,1);
 retry.graph={workspace:'W'};await vm.runInContext('pollLive()',retry);assert.equal(retry.loads,2);
 console.log('Initial failure and un-restored workspace retry full restoration before delta polling PASS (source VM)');
})().catch(e=>{console.error(e);process.exitCode=1});
