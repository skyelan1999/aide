'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const files=['diagramly/vsdx/importer.js','extensions.min.js','integrate.min.js'];
function method(file){
 const text=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');
 if(file===files[0]){
  const a=text.indexOf('PolylineTo.prototype.handle = function'),b=text.indexOf('return PolylineTo;',a);assert.ok(a>=0&&b>a);return text.slice(a,b);
 }
 const type=text.indexOf('.PolylineTo=k;k.__class=');assert.ok(type>=0);
 const marker='/* AIDE Visio polyline budget #177 */';let a=text.lastIndexOf(marker,type);
 if(a<0)a=text.lastIndexOf('a.prototype.handle=function',type);
 const b=text.indexOf(';return a}(g.mxgraph.io.vsdx.geometry.Row)',a);assert.ok(a>=0&&b>a&&b<type);return text.slice(a,b+1);
}
function runtime(file){
 function Polyline(){}
 const com={mxgraph:{io:{vsdx:{mxVsdxUtils:{conversionFactor_$LI$:()=>1}}}}};
 const context={PolylineTo:Polyline,a:Polyline,com,g:com};vm.createContext(context);vm.runInContext(method(file),context);
 vm.runInContext('globalThis.frontRemovals=0;const originalSplice=Array.prototype.splice;Array.prototype.splice=function(start,count,...items){if(start===0&&count===1)frontRemovals++;return originalSplice.call(this,start,count,...items);};',context);
 function run(formula,x=10,y=20){
  const shape={lastX:0,lastY:0,getHeight:()=>100,getWidth:()=>100,getLastMoveX:()=>10,getLastMoveY:()=>80,setLastX(v){this.lastX=v;},setLastY(v){this.lastY=v;}};
  const row=Object.assign(new Polyline(),{x,y,formulaA:formula});return {xml:row.handle(null,shape),shape};
 }
 return {context,run};
}
function main(){
 for(const file of files){
  const r=runtime(file),formula='Polyline(1,1,'+Array.from({length:1000},(_,i)=>`${i%100},${(i+1)%100}`).join(',')+')';
  const result=r.run(formula);
  console.log(JSON.stringify({file,formulaBytes:formula.length,lines:(result.xml.match(/<line /g)||[]).length,frontRemovals:r.context.frontRemovals}));
  assert.equal(r.context.frontRemovals,0,file+' must read forwards without repeated shifting');
  assert.equal(result.xml.split('<line ').length-1,1001);assert.ok(result.xml.endsWith('<line x="10" y="80"/><close/>'));
  assert.equal(r.run('Polyline(1,1,25,75)').xml,'<line x="25" y="25"/><line x="10" y="80"/><close/>');
  assert.throws(()=>r.run(' '.repeat(256*1024+1)),{code:'AIDE_VISIO_LIMIT'});
  assert.throws(()=>r.run('Polyline(1,1,'+'1,2,'.repeat(4096)+'1,2)'),{code:'AIDE_VISIO_LIMIT'});
  const exact=r.run('Polyline(1,1,'+Array.from({length:4096},()=> '1,2').join(',')+')');assert.equal(exact.xml.split('<line ').length-1,4097);
  assert.equal(r.run(null).xml,'');assert.throws(()=>r.run('inh'));
  console.log('PASS',file,'linear reads, preserved normal geometry, formula/point budgets, exact point boundary and legacy no-op/inh behavior');
 }
}
module.exports={method,runtime,files};if(require.main===module)main();
