'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
module.exports=async function(frame,out,theme,file){
 const observed=await frame.evaluate(()=>{
  const ns=com.mxgraph.io.vsdx,cf=ns.mxVsdxUtils.conversionFactor_$LI$();
  function formula(degree,inner){return 'NURBS(1,'+degree+',0,0,'+Array.from({length:inner},(_,i)=>[i/(inner+1),i/(inner+1),i/(inner+1),1].join(',')).join(',')+')';}
  function convert(degree,inner,count=1,raw){
   const rows=Array.from({length:count},(_,i)=>'<Row T="NURBSTo" IX="'+(i+1)+'"><Cell N="X" V="1"/><Cell N="Y" V="1"/><Cell N="A" V="1"/><Cell N="B" V="1"/><Cell N="C" V="0"/><Cell N="D" V="1"/><Cell N="E" V="'+(raw??formula(degree,inner))+'"/></Row>').join('');
   const node=mxUtils.parseXml('<Section N="Geometry" IX="0">'+rows+'</Section>').documentElement;
   const geometry=new ns.mxVsdxGeometry(node);
   const edge=Object.assign(Object.create(ns.VsdxShape.prototype),{geomList:{geomList:[geometry]},getStartXY:()=>({x:0,y:cf}),getEndXY:()=>({x:cf,y:0}),getDimensions:()=>({x:cf,y:cf}),getPinX:()=>0,getPinY:()=>0,getLocPinX:()=>0,getLocPinY:()=>0,getValue:(node,fallback)=>fallback,getCellElement$java_lang_String:()=>null});
   try{return {curve:edge.getControlPoints(cf)};}catch(error){return {code:error.code};}
  }
  const good=convert(3,2);if(!good.curve?.isBezier||!good.curve.points.length||!good.curve.points.every(p=>Number.isFinite(p.x)&&Number.isFinite(p.y)))throw Error('valid NURBS failed');
  for(const result of [convert(12,16),convert(3,255),convert(3,2,1,' '.repeat(65537)),convert(8,254,4)])if(result.code!=='AIDE_VISIO_LIMIT')throw Error('NURBS budget failed '+JSON.stringify(result));
  if(!convert(8,254).curve?.isBezier)throw Error('exact degree/control boundary rejected');
  if(JSON.stringify(convert(3,2).curve)!==JSON.stringify(good.curve))throw Error('normal NURBS retry changed');
  const graph=ownedEditor.graph,cell=graph.insertEdge(graph.getDefaultParent(),null,'Owned NURBS curve',null,null,'edgeStyle=none;curved=1;bezier=1;strokeWidth=3;strokeColor=#1989d1;');
  const geometry=cell.geometry.clone();geometry.sourcePoint=new mxPoint(40,40+2*cf);geometry.targetPoint=new mxPoint(40+2*cf,40);geometry.points=good.curve.points.map(p=>new mxPoint(40+2*p.x,40+2*p.y));graph.model.setGeometry(cell,geometry);ownedUi.actions.get('fitWindow').funct();
  const state=graph.view.getState(cell),box=state.shape.node.getBoundingClientRect();
  return {points:good.curve.points.map(p=>({x:p.x,y:p.y})),rendered:!!state.shape.node.querySelector('path'),visible:box.width>0&&box.height>0&&box.left>=0&&box.top>=0&&box.right<=innerWidth&&box.bottom<=innerHeight};
 });
 assert.ok(observed.rendered&&observed.visible,'converted NURBS edge must render in view');
 await frame.page().screenshot({path:path.join(out,theme+'-visio-nurbs.png')});
 return [{case:'actual XML NURBS rows, edge curve degree/control/formula/shared-work rejection, boundary and normal retry with rendered edge',file,theme,result:'PASS',points:observed.points}];
};
