'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
const cube = fs.readFileSync('examples/file-preview-plugin/cube.obj', 'utf8');
const base = 'v 0 0 0\nv 1 1 1\n';
function runtime(file) {
  const html = fs.readFileSync(file, 'utf8');
  const source = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].at(-1)[1];
  const elements = new Map();let segments = 0, clears = 0;
  const ctx = {clearRect(){clears++;},beginPath(){},moveTo(){},lineTo(){segments++;},stroke(){}};
  const sandbox = {TextEncoder,TextDecoder,Uint8Array,atob,document:{getElementById(id){
    if (!elements.has(id)) elements.set(id, {value:id==='angle'?'30':'',width:800,height:600,getContext:()=>ctx});
    return elements.get(id);
  }},AideWorkbenchClient:{create:()=>({documentInfo:()=>new Promise(()=>{}),onEvent(){}})}};
  vm.createContext(sandbox);vm.runInContext(source,sandbox);
  return {parse:text=>sandbox.parse(text),draw:()=>sandbox.draw(),state:()=>vm.runInContext('[vertices.length,edges.length]',sandbox),get segments(){return segments;},get clears(){return clears;}};
}
function main() {
  for (const file of ['viewer.src.html','viewer.html']) {
    const f = runtime('examples/file-preview-plugin/'+file);
    f.parse(cube);assert.ok(f.state()[0]>0 && f.state()[1]>0);f.draw();assert.equal(f.segments,f.state()[1]);
    for (const [name,text] of [
      ['face indices',base+'f '+'1 '.repeat(2049)],
      ['line indices',base+'l '+'1 '.repeat(2049)],
      ['records without edges',base+'l 1\n'.repeat(100001)],
      ['aggregate edges',base+'l 1 2 1\n'.repeat(50000)+'l 1 2\n'],
      ['line length',base+'#'+'x'.repeat(65536)],
      ['input size',' '.repeat(8*1024*1024+1)],
      ['UTF-8 input size',base+'#界'.repeat(3*1024*1024)],
      ['vertices','v 0 0 0\n'.repeat(100001)]
    ]) {
      assert.throws(()=>f.parse(text),e=>e.code==='AIDE_OBJ_LIMIT',file+': '+name+' must reject');
      assert.equal(JSON.stringify(f.state()),'[0,0]',name+' must not retain partial geometry');
      const before=f.segments;f.draw();assert.equal(f.segments,before,'failed parse cannot draw edges');
      f.parse(cube);assert.ok(f.state()[1]>0,'normal model recovers after rejection');
    }
    f.parse(base+'f '+'1 '.repeat(2048));assert.equal(f.state()[1],2048);
    f.parse(base+'l 1 2 1\n'.repeat(50000));assert.equal(f.state()[1],100000);f.draw();
    f.parse(base+'l -2/-1 -1//1\r\n');assert.equal(f.state()[1],1,'negative/slashed indices');
    assert.throws(()=>f.parse(base+'f 1 99'));assert.equal(JSON.stringify(f.state()),'[0,0]');
    console.log('PASS OBJ bounds, recovery, valid geometry and rendering:',file);
  }
}
if (require.main === module) main();
module.exports={base,cube};
