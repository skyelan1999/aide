'use strict';
module.exports=async function(frame,out,theme) {
  const path=require('node:path');let checks;
  try {
    checks=await frame.evaluate((theme)=>{
      const graph=ownedEditor.graph,parent=graph.getDefaultParent(),owned=[];
      window.ownedGeometryCase={graph,owned};
      const a=graph.insertVertex(parent,null,'Self-loop',60,150,120,50),b=graph.insertVertex(parent,null,'Target',260,150,120,50);owned.push(a,b);
      graph.setSelectionCell(a);const handler=graph.selectionCellsHandler.getHandler(a);
      const hadRaster=Object.hasOwn(window,'raster'),oldRaster=window.raster,grid=graph.gridEnabled,sentinel={owned:true};
      try {
        graph.gridEnabled=true;window.raster=sentinel;handler.startAngle=0;handler.rotationRaster=true;handler.livePreviewActive=false;
        for(const snap of [15,5,1]) {
          handler.startDist=100-(snap===15?0:snap===5?10:30);
          const angle=37*Math.PI/180,state=handler.state,event=new mxMouseEvent(new MouseEvent('mousemove'),state);
          event.graphX=state.getCenterX()+100*Math.sin(angle);event.graphY=state.getCenterY()-100*Math.cos(angle);
          handler.rotateVertex(event);
          if(window.raster!==sentinel || handler.currentAlpha!==Math.round(37/snap)*snap)throw new Error('rotation global leak or snap regression');
        }
      } finally {graph.gridEnabled=grid;if(hadRaster)window.raster=oldRaster;else delete window.raster;}
      const results=[{case:'actual selected vertex handler, 15/5/1 snap and preserved page-global raster',theme,result:'PASS'}];
      const segment=mxEdgeStyle.SegmentConnector,source=graph.view.getState(a),target=graph.view.getState(b);
      try {
        for(const same of [true,false]) {
          let calls=0;mxEdgeStyle.SegmentConnector=function(){calls++;return segment.apply(this,arguments);};
          const state=new mxCellState(graph.view,new mxCell(),{sourceJettySize:20,targetJettySize:80});
          state.absolutePoints=[new mxPoint(0,0),new mxPoint(120,0)];const route=[];
          mxEdgeStyle.OrthConnector(state,source,same?source:target,null,route);
          if(calls!==(same?1:0) || route.some(p=>!Number.isFinite(p.x)||!Number.isFinite(p.y)))throw new Error('loop clearance routing regression');
        }
      } finally {mxEdgeStyle.SegmentConnector=segment;}
      const loop=graph.insertEdge(parent,null,'',a,a,'edgeStyle=orthogonalEdgeStyle;orthogonalLoop=1;sourceJettySize=20;targetJettySize=80;');owned.push(loop);
      const points=graph.view.getState(loop).absolutePoints;
      if(points.length<3 || points.some(p=>!p||!Number.isFinite(p.x)||!Number.isFinite(p.y)))throw new Error('rendered loop is invalid');
      results.push({case:'real router distinguishes self-loop jetty fallback from distinct terminals; rendered self-loop finite',theme,result:'PASS',points:points.map(p=>({x:p.x,y:p.y}))});
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('width','180');svg.setAttribute('height','100');
      const host=document.createElement('div');host.style.cssText='position:fixed;left:40px;top:440px;z-index:1000;width:180px;height:100px;background:#fff;border:1px solid #777';
      host.appendChild(svg);document.body.appendChild(host);ownedGeometryCase.svg=host;
      const canvas=new mxSvgCanvas2D(svg);canvas.setStrokeColor('#0080c0');canvas.setStrokeWidth(2);canvas.begin();canvas.moveTo(10,10);
      canvas.arcTo(0,15,0,false,true,80,60);canvas.lineTo(150,60);canvas.stroke();
      const drawn=svg.querySelector('path'),d=drawn.getAttribute('d').replaceAll(',',' '),length=drawn.getTotalLength(),endpoint=drawn.getPointAtLength(length);
      if(!/L\s*80\s+60/.test(d)||Math.abs(length-(Math.hypot(70,50)+70))>.2||Math.abs(endpoint.x-150)>.01||Math.abs(endpoint.y-60)>.01)throw new Error('zero-radius arc endpoint/path regression');
      results.push({case:'actual SVG canvas zero-radius line, native path length and next endpoint',theme,result:'PASS',d,length,endpoint:{x:endpoint.x,y:endpoint.y}});
      return results;
    },theme);
    await frame.page().screenshot({path:path.join(out,theme+'-geometry.png')});
  } finally {
    await frame.evaluate(()=>{const test=window.ownedGeometryCase;if(test){if(test.svg)test.svg.remove();test.graph.removeCells(test.owned);delete window.ownedGeometryCase;}});
  }
  return checks;
};
