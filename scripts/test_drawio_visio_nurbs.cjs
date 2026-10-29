'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const base='internal/server/web/vendor/drawio/js/',files=['diagramly/vsdx/importer.js','extensions.min.js','integrate.min.js'];
function method(file){
 const text=fs.readFileSync(base+file,'utf8');
 if(file===files[0]){
  const a=text.indexOf('VsdxShape.prototype.getControlPoints ='),b=text.indexOf('VsdxShape.prototype.getStyleFromEdgeShape',a);assert.ok(a>=0&&b>a);return text.slice(a,b);
 }
 const marker='/* AIDE Visio NURBS budgets #178 */';let a=text.indexOf(marker);if(a<0)a=text.indexOf('c.prototype.getControlPoints=function');
 const b=text.indexOf('c.prototype.getStyleFromEdgeShape=',a);assert.ok(a>=0&&b>a);return text.slice(a,b);
}
function fixtureRows(geometry,degree,innerPoints,count=1){
 const formula='NURBS(1,'+degree+',0,0,'+Array.from({length:innerPoints},(_,i)=>[i/(innerPoints+1),i/(innerPoints+1),i/(innerPoints+1),1].join(',')).join(',')+')';
 return Array.from({length:count},()=>Object.assign(new geometry.NURBSTo(),{x:1,y:1,a:1,b:1,c:0,d:1,formulaE:formula}));
}
function runtime(file){
 const geometry={};for(const name of ['ArcTo','DelRow','Ellipse','EllipticalArcTo','InfiniteLine','MoveTo','NURBSTo','PolylineTo','RelCubBezTo','RelEllipticalArcTo','RelLineTo','RelMoveTo','RelQuadBezTo'])geometry[name]=class{};
 function Edge(){}
 const com={mxgraph:{io:{vsdx:{geometry,mxVsdxUtils:{conversionFactor_$LI$:()=>100},mxVsdxConstants:{ANGLE:'Angle',FLIP_X:'FlipX',FLIP_Y:'FlipY'}}}}};
 vm.runInNewContext(method(file),{VsdxShape:Edge,c:Edge,com,g:com,mxPoint:class{constructor(x,y){this.x=x;this.y=y;}}});
 function run(rows){
  const edge=Object.assign(new Edge(),{geomList:{geomList:[{rows,isNoShow:()=>false}]},getStartXY:()=>({x:0,y:100}),getEndXY:()=>({x:100,y:0}),getDimensions:()=>({x:100,y:100}),getPinX:()=>0,getPinY:()=>0,getLocPinX:()=>0,getLocPinY:()=>0,getValue:(node,fallback)=>fallback,getCellElement$java_lang_String:()=>null});
  return edge.getControlPoints(100);
 }
 return {geometry,run};
}
function main(){
 for(const file of files){
  const r=runtime(file),bad=fixtureRows(r.geometry,12,16);let error,result;
  try{result=r.run(bad);}catch(e){error=e;}
  console.log(JSON.stringify({file,degree:12,innerPoints:16,resultPoints:result?.points?.length,error:error?.message,code:error?.code}));
  assert.equal(error?.code,'AIDE_VISIO_LIMIT',file+' rejects high degree before evaluation');
  const good=r.run(fixtureRows(r.geometry,3,2));assert.ok(good.isBezier&&good.points.length);assert.ok(good.points.every(p=>Number.isFinite(p.x)&&Number.isFinite(p.y)));
  assert.throws(()=>r.run(fixtureRows(r.geometry,3,255)),{code:'AIDE_VISIO_LIMIT'});
  const long=fixtureRows(r.geometry,3,2);long[0].formulaE=' '.repeat(65537);assert.throws(()=>r.run(long),{code:'AIDE_VISIO_LIMIT'});
  const exact=r.run(fixtureRows(r.geometry,8,254));assert.ok(exact.points.length);
  assert.throws(()=>r.run(fixtureRows(r.geometry,8,254,4)),{code:'AIDE_VISIO_LIMIT'},'work is shared across NURBS rows on the edge');
  assert.ok(r.run(fixtureRows(r.geometry,3,2)).points.length,'a new edge conversion has a fresh budget');
  console.log('PASS',file,'normal cubic, degree/control/formula limits, exact boundaries, cumulative work and independent retry');
 }
}
module.exports={method,runtime,fixtureRows,files};if(require.main===module)main();
