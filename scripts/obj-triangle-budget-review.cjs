'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawnSync}=require('node:child_process');
const THREEBase=require('../internal/server/web/vendor/three.min.js');
const header='v 0 0 0\nv 1 0 0\nv 1 1 0\nv 0 1 0\nv 0 0 1\n';
function runtime(file=process.env.AIDE_OBJ_VENDOR_BASELINE||'internal/server/web/vendor/OBJLoader.js'){
 let s=fs.readFileSync(file,'utf8');const marker='addFace: function ( a, b, c, ua, ub, uc, na, nb, nc ) {';assert.equal(s.split(marker).length,2);s=s.replace(marker,marker+'globalThis.ownedObjProbe.faces++;');
 const split='const vertexData = lineData.split( /\\s+/ );';assert.equal(s.split(split).length,2);s=s.replace(split,"globalThis.ownedObjProbe.tokenizations++;if(lineData.length>100000)throw Error('owned large token allocation blocked');"+split);
 const scope={THREE:{...THREEBase},console,ownedObjProbe:{faces:0,tokenizations:0}};vm.createContext(scope);vm.runInContext(s,scope);return scope;
}
function triangles(object){let n=0;object.traverse(node=>{if(node.isMesh)n+=node.geometry.attributes.position.count/3});return n;}
function geometry(object){const out=[];object.traverse(node=>{if(node.geometry)out.push({name:node.name,type:node.type,attributes:Object.fromEntries(Object.entries(node.geometry.attributes).map(([k,a])=>[k,Array.from(a.array)])),groups:node.geometry.groups})});return JSON.parse(JSON.stringify(out));}
function pressure(){const c=runtime(),source=header+'f '+'1 '.repeat(3000003);let error;try{new c.THREE.OBJLoader().parse(source,3000000)}catch(e){error=e}assert.equal(error?.code,'AIDE_OBJ_LIMIT',error?.message||'large face accepted');assert.equal(c.ownedObjProbe.faces,0);assert.equal(c.ownedObjProbe.tokenizations,0);console.log(JSON.stringify({inputBytes:source.length,faces:0,tokenizations:0,error:error.code}));}
async function main(){
 const rows=[];async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message})}}
 await test('n-gon before token and vertex expansion',()=>{const c=runtime();let error;try{new c.THREE.OBJLoader().parse(header+'f 1 2 3 4 5\n',2)}catch(e){error=e}assert.equal(error?.code,'AIDE_OBJ_LIMIT');assert.equal(c.ownedObjProbe.faces,0);assert.equal(c.ownedObjProbe.tokenizations,0);});
 await test('budget spans object and material declarations',()=>{const c=runtime();let error;try{new c.THREE.OBJLoader().parse(header+'o first\nf 1 2 3\no second\nusemtl owned\nf 1 3 4\n',1)}catch(e){error=e}assert.equal(error?.code,'AIDE_OBJ_LIMIT');assert.equal(c.ownedObjProbe.faces,1);assert.equal(c.ownedObjProbe.tokenizations,1);});
 await test('normal geometry and exact budget controls',()=>{const fixtures=[fs.readFileSync('examples/file-preview-plugin/cube.obj','utf8'),header+'f 1 2 3 4 5\n',header+'vt 0 0\nvn 0 0 1\nf -5/-1/-1 -4/-1/-1 -3/-1/-1\n',header+'f1 2 3\n',header+'f 1 2 \\\r\n3 4\n',header+'f 1\t2\u00a03\n',header+'f 1 2\n'];for(const source of fixtures){const c=runtime(),legacy=new c.THREE.OBJLoader().parse(source),n=triangles(legacy),bounded=new c.THREE.OBJLoader().parse(source,n);assert.deepEqual(geometry(bounded),geometry(legacy));}return{controls:fixtures.length};});
 await test('invalid budgets and recovery',()=>{const c=runtime(),loader=new c.THREE.OBJLoader();for(const budget of [-1,NaN,1.5,Number.MAX_SAFE_INTEGER+1])assert.throws(()=>loader.parse(header+'f 1 2 3',budget),/budget/);assert.throws(()=>loader.parse(header+'f 1 2 3',0),e=>e.code==='AIDE_OBJ_LIMIT');assert.equal(triangles(loader.parse(header+'f 1 2 3',1)),1);});
 await test('production URDF loader passes remaining aggregate budget',async()=>{
  const c=runtime(),s=fs.readFileSync(process.env.AIDE_URDF_REVIEW_BASELINE||'internal/server/web/urdf-preview.js','utf8'),a=s.indexOf('    const track=object=>{'),b=s.indexOf('    function teardown()',a),x=s.indexOf('      const loadMesh=async id=>'),y=s.indexOf('      loader.loadMeshCb=',x);
  assert(a>=0&&b>a&&x>=0&&y>x);Object.assign(c,{LIMITS:{asset:32*1024*1024,triangles:4,colladaLights:16},triangles:3,geometries:new Set(),materials:new Set(),textures:new Set(),objects:new Set(),importedLights:new Set(),wireframe:false,assetRefs:new Map([['mesh','owned.obj']]),assetMaterials:new Map(),resource:async()=>({path:'owned.obj',data:new TextEncoder().encode(header+'f 1 2 3\n')}),filePath:'owned.urdf',current:()=>true,uiTextUrdfPreview:s=>s,TextDecoder,DOMException});
  const passed=[],parse=c.THREE.OBJLoader.prototype.parse;c.THREE.OBJLoader.prototype.parse=function(text,budget){passed.push(budget);return parse.call(this,text,budget)};
  vm.runInContext(s.slice(a,b)+'\n'+s.slice(x,y)+'\nthis.ownedLoad=loadMesh;',c);
  await c.ownedLoad('mesh');assert.equal(c.triangles,4);c.ownedObjProbe.faces=c.ownedObjProbe.tokenizations=0;
  await assert.rejects(c.ownedLoad('mesh'),/三角面|budget/);assert.deepEqual(passed,[1,0]);assert.equal(c.ownedObjProbe.faces,0);assert.equal(c.ownedObjProbe.tokenizations,0);assert.equal(c.triangles,4);
 });
 await test('3M cap with large but bounded input and allocation trap',()=>{const run=spawnSync(process.execPath,['--max-old-space-size=192',__filename,'--pressure'],{encoding:'utf8',timeout:8000,maxBuffer:4000});assert.equal(run.status,0,run.stderr||run.stdout);return JSON.parse(run.stdout)});
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_OBJ_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module){if(process.argv.includes('--pressure'))pressure();else main().catch(e=>{console.error(e);process.exitCode=1});}
module.exports={runtime,geometry,triangles,header};
