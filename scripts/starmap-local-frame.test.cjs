const createUiContext=require('./fixtures/ui-i18n-context.cjs');
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const candidate=fs.readFileSync('internal/server/web/starmap.js','utf8');
function harness(source){
 const c=createUiContext(vm, {window:{},cosmicSceneCache:new Map(),liveStableScenes:new Map(),nodeByID:new Map(),renderEdges:[],graph:{nodes:[],edges:[]},seeded:()=>.5,cosmicTexture:()=>null,cloudTexture:()=>null,mapPalette:{nebula:'#000'},tau:Math.PI*2});
 vm.runInContext(fs.readFileSync('internal/server/web/starmap-cosmos.js','utf8'),c);
 vm.runInContext(source.slice(source.indexOf('function cosmicData('),source.indexOf('function cosmicPath(')),c);
 return {c,update(nodes,keep=false){
   if(keep)c.liveStableScenes=new Map(c.cosmicSceneCache);
   c.cosmicSceneCache.clear();c.graph.nodes=nodes;c.nodeByID=new Map(nodes.map(n=>[n.id,n]));c.cosmos=c.window.AideStarCosmos.build(nodes,[]);
   return c.cosmicData(c.cosmos.root.key);
 }};
}
const nodes=Array.from({length:30},(_,i)=>({id:'F'+i,name:'file'+i+'.go',path:'pkg/file'+i+'.go',kind:'file',root:'source',source:'fixture',region:'source:fixture',x:.95+(i-15)*.004,y:.4+(i-15)*.003}));
const visible=data=>data.stars.filter(star=>{const x=.57+star.x/star.z,y=.5+star.y/star.z;return x>=.28&&x<=1&&y>=0&&y<=1}).length;
const after=harness(candidate).update(nodes);
assert.equal(visible(after),nodes.length,'framed reference stars must be inside the usable viewport');
const h=harness(candidate);let scene=h.update([]);assert.equal(scene.frame.count,0);
scene=h.update(nodes.slice(0,1),true);assert.equal(scene.stars.length,1);assert.equal(scene.stars[0].x,0);
const first=scene.stars[0],frame=scene.frame;scene=h.update(nodes,true);
assert.strictEqual(scene.frame,frame,'incremental discovery must retain its initial nonempty frame');
assert.equal(scene.stars.find(s=>s.n.id===first.n.id).x,first.x,'existing node position must not jump');
assert.equal(visible(scene),nodes.length,'an empty/one-node first batch must not strand later nodes offscreen');
const positions=new Map(scene.stars.map(s=>[s.n.id,{x:s.x,y:s.y}]));scene=h.update(nodes.slice(0,15),true);
for(const star of scene.stars)assert.deepEqual({x:star.x,y:star.y},positions.get(star.n.id));
const single=harness(candidate).update(nodes.slice(0,1));assert.equal(single.stars[0].x,0);assert.equal(single.stars[0].y,0);
// Exercise the actual reconciliation function: a pending live batch must not
// reuse another source's root-scene frame when the user switches the filter.
const recon=harness(candidate),workspaceNodes=nodes.map(n=>({...n,id:'W'+n.id,x:n.x-1,region:'workspace',root:'workspace',source:''}));
const prior=recon.update(workspaceNodes);
Object.assign(recon.c,{cosmicEnabled:true,locked:false,AideStarCosmos:recon.c.window.AideStarCosmos,graphGeneration:2,layoutGeneration:2,region:'source:fixture',codeView:false,edgeMode:'all',focusCode:false,selected:null,callDepth:'all',cosmicScopeKey:'1|workspace|false|all|false||',liveReconcile:true,cosmicScene:prior.scene,graphFade:null,renderNodes:nodes,cosmicFocusID:'',cosmicHover:'',cosmicSelected:'',syncCosmicNav(){}});
recon.c.graph.nodes=nodes;recon.c.nodeByID=new Map(nodes.map(n=>[n.id,n]));
vm.runInContext(candidate.slice(candidate.indexOf('function cosmicPath('),candidate.indexOf('function cosmicPreferred(')),recon.c);
recon.c.enterCosmic=key=>{recon.c.lastData=recon.c.cosmicData(key)};
vm.runInContext(candidate.slice(candidate.indexOf('function ensureCosmicView('),candidate.indexOf('function cosmicLocate(')),recon.c);
vm.runInContext('ensureCosmicView()',recon.c);
assert.ok(recon.c.lastData,'a source filter change must enter a fresh local scene');assert.equal(visible(recon.c.lastData),nodes.length);
console.log('PASS reference viewport frame, empty/one/many batches, retained positions and source filter reconciliation');
