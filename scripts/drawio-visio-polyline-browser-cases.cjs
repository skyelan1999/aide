'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
module.exports=async function(frame,out,theme,file){
 const observed=await frame.evaluate(()=>{
  function pathFor(formula){
   const node=mxUtils.parseXml('<Section N="Geometry" IX="0"><Row T="PolylineTo" IX="1"><Cell N="X" V="0.8"/><Cell N="Y" V="0.6"/><Cell N="A" V="'+formula+'"/></Row></Section>').documentElement;
   const geometry=new com.mxgraph.io.vsdx.mxVsdxGeometry(node);
   const shape={updates:0,getHeight:()=>100,getWidth:()=>100,getLastMoveX:()=>-1,getLastMoveY:()=>-1,setLastX(){this.updates++;},setLastY(){this.updates++;}};
   try{return {xml:geometry.getPathXML(null,shape),updates:shape.updates};}catch(error){return {code:error.code,updates:shape.updates};}
  }
  const good=pathFor('Polyline(1,1,0.1,0.2,0.2,0.4)');
  if(!good.xml||good.xml.includes('NaN')||good.xml.split('<line ').length-1!==3)throw Error('normal polyline conversion failed '+JSON.stringify(good));
  for(const formula of [' '.repeat(262145),'Polyline(1,1,'+'1,2,'.repeat(4096)+'1,2)']){
   const bad=pathFor(formula);if(bad.code!=='AIDE_VISIO_LIMIT'||bad.updates!==0)throw Error('polyline budget failed '+JSON.stringify(bad));
  }
  const retry=pathFor('Polyline(1,1,0.1,0.2,0.2,0.4)');if(retry.xml!==good.xml)throw Error('normal retry changed geometry');
  const stencil=mxUtils.parseXml('<shape name="owned-polyline" w="100" h="100"><foreground><path><move x="0" y="100"/>'+good.xml+'</path><stroke/></foreground></shape>');
  mxStencilRegistry.addStencil('owned.polyline',new mxStencil(stencil.documentElement));
  const graph=ownedEditor.graph,cell=graph.insertVertex(graph.getDefaultParent(),null,'Owned Visio polyline',40,40,240,240,'shape=owned.polyline;strokeColor=#1989d1;strokeWidth=3;fillColor=none;');
  ownedUi.actions.get('fitWindow').funct();
  const state=graph.view.getState(cell),box=state.shape.node.getBoundingClientRect();
  return {xml:good.xml,rendered:state.shape.stencil.desc.getAttribute('name')==='owned-polyline',visible:box.width>0&&box.height>0&&box.left>=0&&box.top>=0&&box.right<=innerWidth&&box.bottom<=innerHeight};
 });
 assert.ok(observed.rendered&&observed.visible,'actual converted polyline stencil is visible');
 await frame.page().screenshot({path:path.join(out,theme+'-visio-polyline.png')});
 return [{case:'actual XML RowFactory/geometry/PolylineTo conversion, formula/point rejection before shape updates, retry and rendered path',file,theme,result:'PASS',path:observed.xml}];
};
