'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(process.argv[2]||'internal/server/web/starmap.js','utf8');
const graph={nodes:Array.from({length:180},(_,i)=>({id:'N'+i,name:'file'+i+'.go',path:'docs/file'+i+'.go',kind:'file',region:'context',root:'context',source:'fixture'})),edges:Array.from({length:179},(_,i)=>({from:'N'+i,to:'N'+(i+1),kind:'mention'}))};
const metrics={drawImage:0,fill:0,arc:0,save:0,restore:0,dustCalls:0,zonesExamined:0,infraredPoints:0};
const ctx=new Proxy({}, {get(t,key){if(key in t)return t[key];return (...args)=>{if(key in metrics)metrics[key]++;if(key==='createRadialGradient'||key==='createLinearGradient')return {addColorStop(){}};};},set(t,k,v){t[k]=v;return true;}});
const c=vm.createContext({window:{},performance,console,graph,ctx,width:1325,height:940,zoom:1,yaw:0,pitch:.1,cosmicAimX:0,cosmicAimY:0,skyFloat:{x:0,y:0},tau:Math.PI*2,clamp:(x,a,b)=>Math.max(a,Math.min(b,x)),easing:x=>x,clusters:[],nodeByID:new Map(),edgeAdjacency:new Map(),highlightedStarIDs:new Set(),structure:null,codeView:false,selected:null,region:'context',edgeMode:'all',focusCode:false,callDepth:'all',searchQuery:'',resultIDs:new Set(),renderNodes:[],renderEdges:[],edgeBatches:[],renderKey:'',renderRevision:1,focusCache:null,focusKey:'',graphGeneration:1,layoutGeneration:0,cosmicEnabled:true,locked:false,liveReconcile:false,liveStableScenes:new Map(),cosmicSceneCache:new Map(),cosmicDustFrameCache:null,cosmicTextureCache:new Map(),cosmicScopeKey:'',cosmicScene:null,cosmicKey:'',cosmos:null,cosmicRecords:[],cosmicStars:[],cosmicStarEdges:[],cosmicLinks:[],cosmicFlight:null,cosmicFocusID:'',cosmicHover:'',cosmicSelected:'',graphFade:null,cosmicLensOwnerByStar:new Map(),cosmicLensHits:[],cosmicLensedStarIDs:new Set(),cosmicLocalHits:[],cosmicGates:[],cosmicCallState:null,projection:[],liveGhosts:[],liveAnimationUntil:0,animate:true,mapPalette:{light:false,highContrast:false,star:'#abcdef',nebula:'#123456',file:'#123456',reference:'#123456',session:'#123456'},glowSprite:()=>({}),cloudTexture:()=>({}),cosmicTexture:()=>null,colors:()=> '#123456',motionEnabled:()=>true,focusIDs:()=>null,updateLayerLabels(){},syncCosmicNav(){},invalidateView(){c.renderRevision++;c.renderKey='';},drawLiveGhosts(){},drawCosmicLocalSystem(){},drawBlackHole(){},drawBlackHoleLensOverlay(){},skyText:()=> '#fff',skyMuted:()=> '#aaa',infrared:{enabled:false,transitioning:false,draw(ctx,points){metrics.infraredPoints+=points.length;}}});
for(const name of ['starmap-physics','starmap-optics','starmap-structure','starmap-cosmos','starmap-gravity','starmap-lensing'])vm.runInContext(fs.readFileSync('internal/server/web/'+name+'.js','utf8'),c);
c.AideStarCosmos=c.window.AideStarCosmos;
const extract=(from,to)=>vm.runInContext(source.slice(source.indexOf(from),source.indexOf(to)),c);
extract('function seeded(','function skyHash(');
extract('const stellarBands=','function syncMapPalette(');
extract('function birthAlpha(','function liveReady(');
extract('function cosmicData(','function cosmicPath(');
extract('function cosmicPath(','function cosmicPreferred(');
extract('function ensureCosmicView(','function cosmicLocate(');
extract('function cosmicLensDistance(','function drawCosmicLocalSystem(');
extract('function edgeInView(','function draw(now=');
extract('function layout(','function eligibleEdge(');
extract('function eligibleEdge(','function focusIDs(');
extract('function refreshView(','function updateLayerLabels(');
extract('function drawCosmos(','function relationshipOverview(');
c.enterCosmic=key=>{const data=c.cosmicData(key);c.cosmicKey=key;c.cosmicScene=data.scene;c.cosmicRecords=data.records;c.cosmicStars=data.stars;c.cosmicStarEdges=data.edges;};
let started=performance.now();c.layout(true);c.refreshView();c.ensureCosmicView();const setupMs=performance.now()-started;
const original=c.cosmicDustTransmission;c.cosmicDustTransmission=(x,y,z,index)=>{metrics.dustCalls++;metrics.zonesExamined+=(index?.cells.get(Math.floor(x/index.cellSize)+':'+Math.floor(y/index.cellSize))||[]).length;return original(x,y,z,index);};

const data=c.cosmicSceneCache.get(c.cosmicKey);assert.equal(c.cosmicStars.length,graph.nodes.length,'all nodes stay in scene');assert.ok(data.gas.length>0,'fixture exercises real gas');
function frame(now=10000){c.drawCosmos(now,now);}
frame();const firstDust=metrics.dustCalls;assert.ok(firstDust>0);
frame(10034);assert.equal(metrics.dustCalls,firstDust,'stationary frame reuses actual dust samples');
assert.ok(c.cosmicStars.some(star=>c.cosmicDustFrame(data).samples.get(star)<.999),'fixture has nontrivial absorption');
const firstIndex=c.cosmicDustFrame(data).index;frame(10050);assert.strictEqual(c.cosmicDustFrame(data).index,firstIndex);assert.equal(metrics.infraredPoints,0,'disabled infrared allocates no points');
c.infrared.enabled=true;frame(10068);assert.equal(metrics.infraredPoints,c.cosmicStars.length,'enabled infrared sees every source');
c.infrared.enabled=false;c.infrared.transitioning=true;frame(10102);assert.equal(metrics.infraredPoints,c.cosmicStars.length*2,'fade-out retains all inputs');c.infrared.transitioning=false;
// Independent all-zone Gaussian integration, without the cell index or cache.
function transmission(x,y,z,cx,cy){let tau=0;for(const zone of data.gas){const zx=cx+(zone.x+c.cosmicAimX+c.yaw*.22)*c.width*c.zoom/zone.z,zy=cy+(zone.y+c.cosmicAimY+(c.pitch-.1)*.18)*c.height*c.zoom/zone.z,sx=Math.max(22,zone.spread*c.width*3.1*c.zoom/zone.z*.25),sy=Math.max(16,sx*.54),r2=((x-zx)/sx)**2+((y-zy)/sy)**2;if(z>zone.z+.008&&r2<9)tau+=zone.opticalDepth*Math.exp(-.5*r2);}return c.window.AideCosmicPhysics.beerLambert(tau);}
for(const key of ['width','height','zoom','yaw','pitch','cosmicAimX','cosmicAimY']){const old=c[key],prior=c.cosmicDustFrame(data);c[key]=old+(key==='width'||key==='height'?17:.13);frame(10200);const next=c.cosmicDustFrame(data);assert.notStrictEqual(next,prior,key+' invalidates samples');const cx=c.width*(.57+c.skyFloat.x),cy=c.height*(.5+c.skyFloat.y);for(const star of c.cosmicStars){const sampled=c.cosmicDustSample(next,star,star.p.x,star.p.y,star.z,cx,cy);assert.ok(Math.abs(sampled-transmission(star.p.x,star.p.y,star.z,cx,cy))<1e-11,key+' matches uncached optical integration');}c[key]=old;}
frame(10300);const prior=c.cosmicDustFrame(data),beforeDrift=metrics.dustCalls;c.skyFloat.x=.03;c.skyFloat.y=-.04;frame(10334);assert.strictEqual(c.cosmicDustFrame(data),prior,'common sky drift preserves index');assert.equal(metrics.dustCalls,beforeDrift,'common drift preserves samples');
for(const star of c.cosmicStars){const cx=c.width*(.57+c.skyFloat.x),cy=c.height*(.5+c.skyFloat.y);assert.ok(Math.abs(c.cosmicDustSample(prior,star,star.p.x,star.p.y,star.z,cx,cy)-transmission(star.p.x,star.p.y,star.z,cx,cy))<1e-11,'drift matches direct physics');}
assert.notStrictEqual(c.cosmicDustFrame({...data}),prior,'new scene identity invalidates');const priorGas=c.cosmicDustFrame(data);data.gas=[...data.gas];assert.notStrictEqual(c.cosmicDustFrame(data),priorGas,'rebuilt gas invalidates');
c.selected=c.cosmicStars[0].n;const beforeActive=metrics.dustCalls;frame(10400);assert.ok(metrics.dustCalls>beforeActive,'active incident relation gets its own transmission');assert.ok(metrics.fill>graph.nodes.length,'stars and active particles draw');const afterActive=metrics.dustCalls;frame(10434);assert.equal(metrics.dustCalls,afterActive,'selected edge samples reuse');
assert.equal(c.cosmicStars.length,graph.nodes.length);assert.equal(c.cosmicStarEdges.length,graph.edges.length);
assert.ok(source.match(/cosmicSceneCache\.clear\(\);cosmicDustFrameCache=null;/g).length>=6,'scene clear paths, including lock, drop samples');
console.log('starmap frame work PASS: actual draw; all sources/edges, idle reuse, camera/scene/gas invalidation, optical equivalence, drift, active particles and infrared fade');
