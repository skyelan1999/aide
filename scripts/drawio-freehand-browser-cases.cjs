'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
const {files,code}=require('./drawio-freehand-source.cjs');
module.exports=async function(frame,out,theme){
 await frame.evaluate(()=>{window.ownedFreehandOriginal={Ctor:mxFreehand,freehand:ownedEditor.graph.freehand,svg:PerfectFreehand.getSvgPathFromStroke,stroke:PerfectFreehand.getStroke,attr:SVGPathElement.prototype.setAttribute};});
 const checks=[];
 try{
  for(const file of files){
   await frame.addScriptTag({content:code(file)});
   for(const perfect of [true,false])for(const smoothing of [1,20]){
    const rect=await frame.evaluate(({perfect,smoothing})=>{
     ownedUi.setFileData('<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel>');
     const graph=ownedEditor.graph,add=graph.addListener,addMouse=graph.addMouseListener,listeners=[];let mouse;
     graph.addListener=function(name,fn){listeners.push(fn);return add.apply(this,arguments);};graph.addMouseListener=function(fn){mouse=fn;return addMouse.apply(this,arguments);};
     let tool;try{tool=new mxFreehand(graph);}finally{graph.addListener=add;graph.addMouseListener=addMouse;}
     const state={tool,mouse,listeners,smoothing,phase:'short',preview:null,stats:{max:0,work:0,commit:0,commands:0}};window.ownedFreehandCase=state;graph.freehand=tool;
     PerfectFreehand.getSvgPathFromStroke=function(points){if(state.phase==='long'){state.stats.max=Math.max(state.stats.max,points.length);state.stats.work+=points.length;}return ownedFreehandOriginal.svg.apply(this,arguments);};
     PerfectFreehand.getStroke=function(points){if(state.phase==='long')state.stats.commit=Math.max(state.stats.commit,points.length);return ownedFreehandOriginal.stroke.apply(this,arguments);};
     SVGPathElement.prototype.setAttribute=function(name,value){if(state.phase==='long'&&name==='d'&&state.preview&&(this===state.preview||state.preview.contains(this)))state.stats.commands=Math.max(state.stats.commands,(String(value).match(/ L/g)||[]).length+1);return ownedFreehandOriginal.attr.apply(this,arguments);};
     tool.setPerfectFreehandMode(perfect);tool.setStrokeColor('#1989d1');tool.setAutoScroll(false);tool.setOpenFill(false);tool.setSmoothing(4);tool.startDrawing();
     const r=graph.container.getBoundingClientRect();return {left:r.left,top:r.top,width:r.width,height:r.height};
    },{perfect,smoothing});
    try{
     const iframe=await frame.frameElement(),box=await iframe.boundingBox();await iframe.dispose();assert.ok(box);
     await frame.page().mouse.move(box.x+rect.left+20,box.y+rect.top+80);await frame.page().mouse.down();await frame.page().mouse.move(box.x+rect.left+Math.min(140,rect.width-30),box.y+rect.top+140,{steps:40});await frame.page().mouse.up();
     const short=await frame.evaluate(()=>{ownedFreehandCase.tool.stopDrawing();return ownedEditor.graph.getChildVertices(ownedEditor.graph.getDefaultParent()).length;});assert.equal(short,1,'actual mouse stroke inserts one vertex');
     const during=await frame.evaluate(()=>{
      const state=ownedFreehandCase,graph=ownedEditor.graph;state.tool.setSmoothing(state.smoothing);state.tool.startDrawing();state.phase='long';state.stats={max:0,work:0,commit:0,commands:0};
      const r=graph.container.getBoundingClientRect(),n=1500;
      function me(i,type){return new mxMouseEvent(new MouseEvent(type,{clientX:r.left+20+i/n*Math.min(260,r.width-40),clientY:r.top+210+Math.sin(i/25)*35,button:0,buttons:type==='mouseup'?0:1,cancelable:true}));}
      state.event=me;state.mouse.mouseDown(graph,me(0,'mousedown'));state.preview=graph.view.canvas.ownerSVGElement.lastElementChild;
      for(let i=1;i<=n;i++)state.mouse.mouseMove(graph,me(i,'mousemove'));
      return {stats:state.stats,vertices:graph.getChildVertices(graph.getDefaultParent()).length,previewPaths:state.preview.querySelectorAll('path').length};
     });
     assert.equal(during.vertices,1,'preview chunks do not insert model cells');if(perfect){assert.ok(during.stats.max<=(smoothing===1?512:520));assert.ok(during.stats.work<563625);}else assert.ok(during.stats.commands<=(smoothing===1?512:520));
     if(file==='app.min.js')await frame.page().screenshot({path:path.join(out,theme+'-freehand-'+(perfect?'perfect':'line')+'-'+smoothing+'-preview.png')});
     const committed=await frame.evaluate(perfect=>{
      const state=ownedFreehandCase,graph=ownedEditor.graph;state.mouse.mouseUp(graph,state.event(1500,'mouseup'));state.tool.stopDrawing();
      const cells=graph.getChildVertices(graph.getDefaultParent()),cell=cells.at(-1),style=cell.style,id=cell.id;
      const encoded=style.match(/shape=stencil\(([^)]+)\)/)?.[1];if(!encoded)throw Error('freehand stencil missing');
      const xml=Graph.decompress(encoded),lines=(xml.match(/<line /g)||[]).length;
      ownedUi.actions.get('undo').funct();const afterUndo=graph.getChildVertices(graph.getDefaultParent()).length;
      ownedUi.actions.get('redo').funct();const restored=graph.model.getCell(id);if(!restored||restored.style!==style)throw Error('freehand redo changed stencil');
      ownedUi.actions.get('fitWindow').funct();const rendered=!!graph.view.getState(restored)?.shape?.node;
      return {vertices:cells.length,afterUndo,afterRedo:graph.getChildVertices(graph.getDefaultParent()).length,previewDetached:!state.preview.parentNode,stats:state.stats,lines,rendered};
     },perfect);
     assert.equal(committed.vertices,2);assert.equal(committed.afterUndo,1);assert.equal(committed.afterRedo,2);assert.ok(committed.previewDetached&&committed.rendered);if(perfect)assert.equal(committed.stats.commit,1501);else assert.equal(committed.lines,1500);
     if(file==='app.min.js')await frame.page().screenshot({path:path.join(out,theme+'-freehand-'+(perfect?'perfect':'line')+'-'+smoothing+'-committed.png')});
     checks.push({case:'real mouse short stroke plus finite actual-listener long stroke bounded preview, complete single-cell commit, cleanup and undo/redo',file,perfect,smoothing,theme,result:'PASS',preview:during,committed});
    }finally{await frame.evaluate(()=>{const state=window.ownedFreehandCase;if(state){state.tool.stopDrawing();if(state.preview?.parentNode)state.preview.parentNode.removeChild(state.preview);ownedEditor.graph.removeMouseListener(state.mouse);for(const fn of state.listeners)ownedEditor.graph.removeListener(fn);}ownedEditor.graph.freehand=ownedFreehandOriginal.freehand;PerfectFreehand.getSvgPathFromStroke=ownedFreehandOriginal.svg;PerfectFreehand.getStroke=ownedFreehandOriginal.stroke;SVGPathElement.prototype.setAttribute=ownedFreehandOriginal.attr;delete window.ownedFreehandCase;});}
   }
  }
 }finally{await frame.evaluate(()=>{window.mxFreehand=ownedFreehandOriginal.Ctor;delete window.ownedFreehandOriginal;});}
 return checks;
};
