'use strict';
module.exports=async function(frame,theme) {
  const checks=await frame.evaluate((theme)=>{
    const graph=ownedEditor.graph,parent=graph.getDefaultParent(),results=[];
    for(const id of ['constructor','normal','__proto__','toString','hasOwnProperty']) {
      const cell=graph.insertVertex(parent,id,'Clone '+id,40,130,150,30);
      const fields={cells:[id,'valueOf','*'],excludeCells:[id],layers:[id]};
      graph.setLinkForCell(cell,'data:action/json,'+JSON.stringify({actions:[{toggle:fields}]}));
      const original=graph.getLinkForCell(cell),clone=graph.duplicateCells([cell],false)[0];
      const link=JSON.parse(graph.getLinkForCell(clone).slice(17)).actions[0].toggle;
      if(JSON.stringify(link)!==JSON.stringify({cells:[clone.id,'valueOf','*'],excludeCells:[clone.id],layers:[clone.id]})) throw new Error('corrupt cloned link '+id);
      if(graph.getLinkForCell(cell)!==original)throw new Error('source link mutated');
      graph.removeCells([cell,clone]);
    }
    results.push({case:'actual duplicateCells remaps custom links, special IDs, wildcards and unmapped inherited names',theme,result:'PASS'});
    return results;
  },theme);
  try {
    const box=await frame.locator('.geDiagramContainer').boundingBox();
    await frame.page().mouse.move(box.x+box.width/2,box.y+box.height/2);
    await frame.evaluate(()=>{
      const graph=ownedEditor.graph,cell=graph.insertVertex(graph.getDefaultParent(),null,'Connection marker',40,130,120,40);
      const original=graph.getAllConnectionConstraints,handler=new mxConstraintHandler(graph);
      graph.getAllConnectionConstraints=function(state){return state.cell===cell?[new mxConnectionConstraint(new mxPoint(.5,0),false),new mxConnectionConstraint(new mxPoint(.5,1),false)]:original.apply(this,arguments);};
      const state=graph.view.getState(cell),event=new mxMouseEvent(new MouseEvent('mousemove'),state);
      event.graphX=state.x+state.width/2;event.graphY=state.y;
      window.ownedConstraintCase={graph,cell,original,handler,resets:0};
      const reset=handler.reset;handler.reset=function(){ownedConstraintCase.resets++;return reset.apply(this,arguments);};
      handler.update(event,true);
      if(!handler.focusIcons || handler.focusIcons.length!==2 || !handler.focusHighlight) throw new Error('markers/highlight not created');
    });
    // Outside the iframe, causing a real browser mouseleave on the graph container.
    await frame.page().mouse.move(5,980);
    const left=await frame.evaluate(()=>{
      const test=ownedConstraintCase,h=test.handler;
      if(h.currentFocus!==null || h.focusIcons!==null || h.focusHighlight!==null || test.resets!==1) throw new Error('markers survived real pointer leave');
      h.destroy();const before=test.resets;
      test.graph.container.dispatchEvent(new MouseEvent('mouseleave'));
      if(test.resets!==before)throw new Error('mouseleave listener leaked after destroy');
      return {resets:before,iconsCleared:true,listenerRemoved:true};
    });
    checks.push({case:'real pointer leaves graph: clear icons/highlight and remove exact listener on destroy',theme,result:'PASS',...left});
  } finally {
    await frame.evaluate(()=>{
      const test=window.ownedConstraintCase;
      if(test){test.handler.destroy();test.graph.getAllConnectionConstraints=test.original;test.graph.removeCells([test.cell]);delete window.ownedConstraintCase;}
    });
  }
  return checks;
};
