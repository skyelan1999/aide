const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(process.argv[2]||'internal/server/web/starmap.js','utf8');
const code=source.slice(source.indexOf('const stellarBands='),source.indexOf('function syncMapPalette('));
const ctx={mapPalette:{light:false,highContrast:false},highlightedStarIDs:new Set(),codeView:false,glowSprite:()=>({}),window:{},graph:{nodes:[],edges:[]},nodeByID:new Map()};vm.createContext(ctx);vm.runInContext(code,ctx);
const oldCapNodes=[64,128].map(connectionCount=>{const node={id:'cap'+connectionCount,connectionCount};ctx.applyStellarColor(node);return node;});
assert.ok(ctx.stellarVisual(oldCapNodes[1],0).radius>ctx.stellarVisual(oldCapNodes[0],0).radius,'doubling high connection counts must still increase visible size');
assert.equal(typeof ctx.stellarScale,'function');
for(let level=1;level<=52;level++){
 const threshold=2**level-1,m=ctx.stellarScale(threshold);assert.equal(m.level,level);assert.equal(m.min,threshold);assert.equal(m.next,threshold+2**level);
 assert.equal(ctx.stellarScale(threshold-1).level,level-1,'rank stays below its cumulative threshold');
 assert.ok(Math.abs(m.radius**3/(1.4**3*threshold)-1)<1e-12,'volume is proportional to cumulative degree');
}
for(const [count,level]of [[0,0],[1,1],[2,1],[3,2],[4,2],[6,2],[7,3],[8,3],[14,3],[15,4],[31,5],[63,6],[127,7]])assert.equal(ctx.stellarScale(count).level,level);
for(const bad of [0,-1,NaN,Infinity,undefined,'8']){const m=ctx.stellarScale(bad);assert.equal(m.level,0);assert.equal(m.radius,.72);assert.equal(m.next,1);}
assert.equal(ctx.stellarScale(Number.MAX_SAFE_INTEGER).level,53);assert.equal(ctx.stellarScale(3.9).level,2);
const rows=[0,1,2,3,4,8,16,32,64,128,1024].map(count=>{const n={id:'n'+count,connectionCount:count};ctx.applyStellarColor(n);return n;});
for(const chosen of [false,true])for(const incoming of [false,true])for(const highlight of [false,true]){
 ctx.highlightedStarIDs=highlight?new Set(rows.map(n=>n.id)):new Set();let last=0;
 for(const node of rows){const r=ctx.stellarVisual(node,0,chosen,incoming).radius;assert.ok(r>last,'all interaction states preserve strict degree/size ordering');last=r;}
}
ctx.highlightedStarIDs=new Set();ctx.codeView=true;assert.ok(ctx.stellarVisual(rows[4],8).radius<ctx.stellarVisual(rows[4],0).radius,'call-depth attenuation retained');
ctx.codeView=false;assert.ok(ctx.stellarVisual(rows.at(-1),0).radius>ctx.stellarVisual(rows.at(-2),0).radius,'large degrees do not hit old 63-degree size cap');
ctx.graph={nodes:[{id:'A'},{id:'B'},{id:'C'},{id:'P'},{id:'Q'}],edges:[{from:'A',to:'B',kind:'calls'},{from:'B',to:'A',kind:'calls'},{from:'A',to:'B',kind:'calls'},{from:'A',to:'A',kind:'calls'},{from:'A',to:'C',kind:'contains'},{from:'B',to:'C',kind:'defines'},{from:'A',to:'missing',kind:'calls'}]};ctx.nodeByID=new Map(ctx.graph.nodes.map(n=>[n.id,n]));ctx.prepareStellarConnections();assert.equal(ctx.nodeByID.get('A').connectionCount,1);assert.equal(ctx.nodeByID.get('B').connectionCount,1);assert.equal(ctx.nodeByID.get('C').stellarLevel,0);
ctx.nodeByID.get('A').parentFile='P';ctx.nodeByID.get('B').parentFile='Q';ctx.prepareStellarConnections();assert.equal(ctx.nodeByID.get('P').connectionCount,1);assert.equal(ctx.nodeByID.get('Q').connectionCount,1,'file rollups keep unique real neighbors');
assert.match(source,/r=stellarRadius\(node\)\*\.85\*mag/,'close-up planets use the same base size');
console.log('Stellar scale PASS: cumulative power-of-two ranks, exact boundaries, proportional volume, all emphasis states, no old size cap, degree evidence and close-up integration');
