'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const context={window:{}};
vm.runInNewContext(fs.readFileSync('internal/server/web/starmap-cosmos.js','utf8'),context);
const nodes=[],edges=[];
function system(file,label){nodes.push({id:file,name:file,kind:'file',path:`${file}.py`,region:'workspace'});const ids=[];for(let i=0;i<4;i++){const id=`${file}-n${i}`;ids.push(id);nodes.push({id,name:`${label} ${i}`,kind:'symbol',symbolKind:'function',parentFile:file,path:`${file}.py`,region:'workspace',connectionCount:i+1});}for(let i=0;i<3;i++)edges.push({from:ids[i],to:ids[i+1],kind:'call_candidate'});return ids;}
const a=system('a','Alpha'),b=system('b','Beta'),c=system('c','Gamma');
edges.push({from:a[0],to:b[0],kind:'call_typed'});edges.push({from:a[1],to:c[1],kind:'imports'});edges.push({from:a[2],to:c[2],kind:'contains'});
const model=context.window.AideStarCosmos.build(nodes,edges),path=model.pathFor(a[0]),systemGroup=path.find(g=>g.depth===6),scene=model.scene(systemGroup.key);
assert(systemGroup,'a local call component should establish a stellar system');
assert.equal(scene.gates.length,2,'only cross-system semantic links create gates');
assert.equal(JSON.stringify(Array.from(scene.gates,g=>g.targetLabel).sort()),JSON.stringify(['Beta 0 · 调用单元','Gamma 0 · 调用单元'].sort()));
assert(scene.gates.every(g=>g.anchorNodeID&&g.count===1));
const nodeScene=model.scene(path.at(-1).key);assert.equal(nodeScene.depth,7);assert.equal(nodeScene.gates.length,scene.gates.length,'node-world close-up should inherit its stellar system navigation gates');
const root=model.scene(model.root.key);assert.equal(root.gates.length,0,'universe view should not expose close-view gates');
console.log('starmap gate model: PASS');
