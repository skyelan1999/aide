const createUiContext=require('./fixtures/ui-i18n-context.cjs');
'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
function harness(providedNodes){
 const els=new Map(),el=id=>{if(!els.has(id))els.set(id,{disabled:false,hidden:false,textContent:'',value:''});return els.get(id);};
 const nodes=providedNodes||[{id:'F1',name:'one.go',kind:'file',region:'workspace',root:'workspace',path:'one.go',text:'old text',readState:'pending',modified:'old',size:3,contentHash:'old-hash',x:.1,y:.2},{id:'F2',name:'two.go',kind:'file',region:'workspace',root:'workspace',path:'two.go',text:'stable',x:.3,y:.4}];
 const graph={workspace:'W',nodes,edges:[{from:'F1',to:'F2',kind:'mention'}],sources:[],warnings:[]};
 const c=createUiContext(vm, {window:{},performance,graph,documentBatch:null,resetDocumentBatch(){c.documentBatch=null;c.resetDocumentCount=(c.resetDocumentCount||0)+1;el('document-next').hidden=true;el('document-progress').hidden=true;},graphGeneration:1,layoutGeneration:1,documentGeneration:0,pathAbort:null,pathResult:null,liveCursor:'R1',liveWireNodes:new Map(nodes.map(n=>[n.id,JSON.stringify(n,(key,value)=>key==='x'||key==='y'?undefined:value)])),selected:null,nodeByID:new Map(nodes.map(n=>[n.id,n])),edgeAdjacency:new Map(),liveGhosts:[],liveStableScenes:new Map(),liveReconcile:false,liveAnimationUntil:0,cosmicStars:[],cosmicSceneCache:new Map(),cosmicScene:null,cosmos:null,cosmicScopeKey:'',cosmicEnabled:true,locked:false,region:'workspace',codeView:false,edgeMode:'all',focusCode:false,callDepth:'all',cosmicKey:'',cosmicFocusID:'',cosmicHover:'',cosmicSelected:'',graphFade:null,renderNodes:nodes,yaw:.4,pitch:.2,zoom:1.5,$:el,motionEnabled:()=>false,mapPalette:{star:'#fff'},layoutCalls:0,modelBuilds:0,resultCalls:0,refreshCalls:0,redrawCalls:0,clearCalls:0,aiAbort:null,documentAbort:null,filePreview:{close(){c.previewClosed=true}},aiExplorer:{updated(){c.aiUpdated=true;},cancel(){c.aiAbort?.abort();}},
  edgeKey:e=>JSON.stringify([e.from,e.to,e.kind,e.line||0,e.column||0,e.evidence||'',e.confidence||'']),
  clearEvidencePath(){c.pathResult=null;c.clearCalls++;},syncGraphChrome(){},revealPanel(){},syncCosmicNav(){},renderCodeInsight(n){c.detailHash=n.contentHash;},invalidateView(){c.viewInvalidated=true;},retrievalMode:()=>c.retrieval||'nodes',nodeResults(){c.resultCalls++;c.searchIDs=c.graph.nodes.filter(n=>n.text?.includes(c.query||'')).map(n=>n.id);},refreshView(){c.refreshCalls++;c.renderNodes=c.graph.nodes.filter(n=>!c.region||n.region===c.region);},progressiveSummary:()=>'',say(){},redraw(){c.redrawCalls++;},nodeDetailText:n=>n.readState==='unavailable'?'unavailable':n.readState==='pending'?'pending':n.text||'',search(){}});
 vm.runInContext(fs.readFileSync('internal/server/web/starmap-cosmos.js','utf8'),c);const real=c.window.AideStarCosmos;
 c.AideStarCosmos=c.window.AideStarCosmos={build(...args){c.modelBuilds++;return real.build(...args);}};
 c.cosmicData=key=>({scene:c.cosmos.scene(key),records:[],stars:c.graph.nodes.map(n=>({n})),edges:[],projection:[]});
 c.enterCosmic=key=>{c.cosmicKey=key;c.cosmicScene=c.cosmos.scene(key);c.cosmicStars=c.graph.nodes.map(n=>({n}));};
 Object.assign(c,{clusters:[],cosmicTextureCache:new Map(),cosmicRecords:[],cosmicLinks:[],cosmicStarEdges:[],cosmicFlight:null,projection:[],seeded:()=>.5,tau:Math.PI*2,colors:()=> '#000',cloudTexture:()=>null,applyStellarColor(){},prepareStellarConnections(){}});
 vm.runInContext(source.slice(source.indexOf('function layout('),source.indexOf('function eligibleEdge(')),c);
 const realLayout=c.layout;c.layout=(...args)=>{c.layoutCalls++;return realLayout(...args);};
 vm.runInContext(source.slice(source.indexOf('const nodeFields='),source.indexOf('const sourceTypes=')),c);
 vm.runInContext(source.slice(source.indexOf('function cosmicPath('),source.indexOf('function cosmicPreferred(')),c);
 vm.runInContext(source.slice(source.indexOf('function ensureCosmicView('),source.indexOf('function cosmicLocate(')),c);
 vm.runInContext(source.slice(source.indexOf('function applyLiveUpdate('),source.indexOf('async function load(')),c);
 const wireNode=n=>Object.fromEntries([...vm.runInContext('nodeFields',c)].filter(k=>n[k]!==undefined).map(k=>[k,n[k]]));
 c.liveWireNodes=new Map(nodes.map(n=>[n.id,JSON.stringify(wireNode(n))]));
 c.ensureCosmicView();c.modelBuilds=0;
 return {c,els,delta(changes={},extra={}){const raw={...wireNode(c.graph.nodes[0]),...changes};return {workspace:'W',revision:'R2',nodes:[JSON.parse(JSON.stringify(raw))],edges:[],removedNodes:[],removedEdges:[],sources:[],warnings:[],...extra};}};
}
const h=harness(),priorModel=h.c.cosmos,priorScene=h.c.cosmicScene,priorNode=h.c.graph.nodes[0];h.c.selected={...priorNode,documentCitation:{hash:'old-hash'}};h.c.pathResult={found:true,nodes:[priorNode],steps:[],revision:'R1'};h.c.query='new';h.c.documentAbort={abort(){h.c.documentAborted=true}};h.c.aiAbort={signal:{aborted:false},abort(){h.c.aiAborted=true}};h.els.set('ask',{disabled:true,textContent:''});
h.c.applyLiveUpdate(h.delta({text:'new text',readState:undefined,size:10,modified:'new',contentHash:'new-hash'}));
assert.equal(h.c.layoutCalls,0,'metadata-only live change must not rebuild full layout');assert.equal(h.c.modelBuilds,0,'metadata-only live change must not rebuild cosmic groups');assert.strictEqual(h.c.cosmos,priorModel);assert.strictEqual(h.c.cosmicScene,priorScene);assert.strictEqual(h.c.graph.nodes[0],priorNode);assert.equal(priorNode.text,'new text');assert.equal('readState'in priorNode,false);assert.equal(h.els.get('detail-text').textContent,'new text');assert.equal(h.c.selected.documentCitation,undefined);assert.equal(h.c.detailHash,'new-hash');assert.deepEqual([...h.c.searchIDs],['F1']);assert.equal(h.c.clearCalls,1);assert.ok(h.c.documentAborted&&h.c.aiUpdated);assert.ok(!h.c.aiAborted,'AI snapshot survives live metadata updates');assert.equal(h.c.documentGeneration,1);assert.deepEqual([h.c.yaw,h.c.pitch,h.c.zoom],[.4,.2,1.5]);
for(const [field,value]of [['name','renamed.go'],['path','other/file.go'],['region','source:other'],['root','context'],['source','new-source'],['origin','new-origin'],['symbolKind','method'],['signature','function changed()']]){const x=harness();x.c.applyLiveUpdate(x.delta({[field]:value}));assert.equal(x.c.layoutCalls,1,field+' change must invalidate structural grouping');assert.equal(x.c.modelBuilds,1);}
for(const extra of [{nodes:[{id:'F3',name:'new.go',kind:'file',region:'workspace',path:'new.go'}]},{nodes:[],removedNodes:['F1']},{edges:[{from:'F2',to:'F1',kind:'call_typed'}]},{removedEdges:[{from:'F1',to:'F2',kind:'mention'}]}]){const x=harness();x.c.applyLiveUpdate(x.delta({},extra));assert.equal(x.c.layoutCalls,1,'node/edge membership change must rebuild');}

{
 const x=harness();x.c.selected={...x.c.graph.nodes[1],documentCitation:{hash:'still-valid'}};x.c.pathResult={found:true,nodes:[x.c.graph.nodes[1]],steps:[],revision:'R1'};
 x.c.applyLiveUpdate(x.delta({readState:'unavailable',text:undefined}));
 assert.equal(x.c.layoutCalls,0);assert.equal(x.c.selected.documentCitation.hash,'still-valid','unrelated selected citation survives');assert.equal(x.c.pathResult.revision,'R2','unchanged path advances to current revision');assert.equal(x.c.graph.nodes[0].text,undefined);assert.equal(x.c.graph.nodes[0].readState,'unavailable');
}
{
 const x=harness();x.c.selected=x.c.graph.nodes[0];x.c.retrieval='original';x.c.applyLiveUpdate(x.delta({text:'new body',readState:undefined}));
 assert.equal(x.c.resultCalls,0,'document search is not silently reissued');assert.equal(x.c.layoutCalls,0);assert.match(x.els.get('document-note').textContent,/更新/);assert.equal(x.els.get('detail-text').textContent,'new body');
}
{
 const x=harness(),old=x.c.cosmos;const reset=x.c.graph.nodes.map(n=>({...n,text:'replacement body'}));
 x.c.applyLiveUpdate(x.delta({}, {reset:true,nodes:reset,edges:x.c.graph.edges}));assert.equal(x.c.layoutCalls,0);assert.strictEqual(x.c.cosmos,old);assert.equal(x.c.graph.nodes.length,2);
 x.c.applyLiveUpdate(x.delta({}, {workspace:'another-workspace',reset:true,nodes:reset,edges:[]}));assert.equal(x.c.layoutCalls,1,'workspace reset never uses previous workspace model');assert.equal(x.c.previewClosed,true,'workspace reset closes owned preview');
}
{
 const x=harness();x.c.applyLiveUpdate(x.delta({}));assert.equal(x.c.layoutCalls,0);assert.equal(x.c.modelBuilds,0);assert.equal(x.c.graphGeneration,1,'wire-equivalent poll keeps data generation');
}


// A removed source must clear its current excerpts/cursor, while an unrelated
// progressive body observation must keep valid results usable.
for(const change of [{removedNodes:['F1'],nodes:[]},{sources:[]},{nodes:[{id:'F1',name:'one.go',kind:'file',region:'workspace',root:'workspace',path:'one.go',origin:'new-origin'}]}]){
 const x=harness();x.c.graph.sources=[{id:'workspace',state:'partial'}];
 const file={...x.c.graph.nodes[0],origin:'old-origin'};x.c.documentBatch={hits:[{file,text:'cached excerpt'}],cursor:'old-cursor'};x.c.selected={...file,documentCitation:{hash:'exact-file-hash'}};
 x.c.applyLiveUpdate(x.delta({}, {sources:[{id:'workspace',state:'partial'}],...change}));
 assert.equal(x.c.documentBatch,null);assert.equal(x.c.resetDocumentCount,1);assert.equal(x.c.previewClosed,true);assert.equal(x.c.selected,null);assert.match(x.els.get('results').textContent,/重新探索/);
}
{
 const x=harness();x.c.documentBatch={hits:[{file:{...x.c.graph.nodes[1]}}],cursor:'valid-cursor'};
 x.c.applyLiveUpdate(x.delta({text:'unrelated refreshed text'},{sources:[{id:'workspace',state:'partial'}]}));
 assert.equal(x.c.documentBatch.cursor,'valid-cursor');assert.equal(x.c.resetDocumentCount,undefined);
}

console.log('PASS metadata-only live updates preserve layout/cosmos/camera and refresh details/search/citations; structural changes still rebuild');
{
 const x=harness();x.c.documentAbort={abort(){x.c.documentAborted=true;}};
 x.c.applyLiveUpdate(x.delta({}, {nodes:[{id:'F3',name:'new.md',kind:'file',region:'workspace',root:'workspace',path:'new.md'}]}));
 assert.equal(x.c.documentAborted,undefined,'additive progress must not cancel a pending document search');
 assert.equal(x.c.documentGeneration,0);
 x.c.applyLiveUpdate(x.delta({}, {nodes:[],edges:[{from:'F1',to:'F3',kind:'mention'}]}));
 assert.equal(x.c.documentAborted,undefined,'new relation alone must not cancel document retrieval');
 x.c.applyLiveUpdate(x.delta({}, {nodes:[],removedNodes:['F3']}));
 assert.equal(x.c.documentAborted,true,'removal invalidates document snapshot');
}
{
 const x=harness();const n=x.c.graph.nodes[0];delete n.text;delete n.contentHash;n.readState='pending';
 x.c.documentAbort={abort(){x.c.documentAborted=true;}};
 x.c.applyLiveUpdate(x.delta({text:'first available body',readState:undefined,contentHash:'first-hash'}));
 assert.equal(x.c.documentAborted,undefined,'first body discovery is additive, not a modification of a known body');
}
{
 const x=harness();x.c.graph.sources=[{id:'workspace',type:'local',state:'partial'}];
 x.c.documentBatch={hits:[{file:{...x.c.graph.nodes[1]}}],cursor:'valid-cursor'};
 x.c.documentAbort={abort(){x.c.documentAborted=true;}};
 x.c.applyLiveUpdate(x.delta({}, {nodes:[{id:'F3',name:'added.md',kind:'file',region:'workspace',root:'workspace',path:'added.md'}],sources:[{id:'workspace',type:'local',state:'partial'}]}));
 assert.equal(x.c.documentBatch.cursor,'valid-cursor','merged preview invalidation preserves valid results during additive background indexing');
 assert.equal(x.c.documentAborted,undefined,'valid continuation remains in flight during additive indexing');
 x.c.selected={...x.c.graph.nodes[1],documentCitation:{hash:'valid-hash'}};
 x.c.applyLiveUpdate(x.delta({}, {nodes:[],sources:[]}));
 assert.equal(x.c.documentBatch,null,'source-only metadata removal clears cached excerpts even without node changes');
 assert.equal(x.c.documentAborted,true);assert.equal(x.c.selected,null);assert.equal(x.c.previewClosed,true);
}
console.log('PASS metadata-only layout preservation and additive-search ownership; structural/content changes retain invalidation');
