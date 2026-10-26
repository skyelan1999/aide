'use strict';
module.exports = async function (frame, out, theme) {
  const assert = require('node:assert/strict'), path = require('node:path');
  const checks = await frame.evaluate(async (theme) => {
    if (!AideDrawioLayouts.installed || !ownedInstalledBeforeMain) throw new Error('layout hooks not installed');
    const graph = ownedEditor.graph, model = graph.model, parent = graph.getDefaultParent(), checks = [];
    const getLayout = graph.layoutManager.getLayout, callbacks = [], owned = new Set();let applies = 0;
    const layout = {prepare(cell,done){callbacks.push({cell,done});}};
    const wait = async () => {for(let i=0;i<100 && callbacks.length===0;i++) await new Promise(resolve=>setTimeout(resolve,5));if(callbacks.length!==1) throw new Error('async layout did not prepare exactly once');};
    try {
      graph.layoutManager.getLayout = function (cell) {return owned.has(cell) ? layout : getLayout.apply(this,arguments);};
      for (const id of ['ordinary','constructor','toString','hasOwnProperty','__proto__']) {
        const cell = graph.insertVertex(parent,id,id,40,130,120,30);owned.add(cell);
        const apply = () => {applies++;graph.scheduleAsyncLayout(cell);const geo=cell.geometry.clone();geo.width++;model.setGeometry(cell,geo);};
        graph.scheduleAsyncLayout(cell);await wait();graph.scheduleAsyncLayout(cell);graph.scheduleAsyncLayout(cell);
        callbacks.shift().done(null,apply);await wait();callbacks.shift().done(null,apply);
        graph.scheduleAsyncLayout(cell);await wait();callbacks.shift().done(null,apply);
        await new Promise(resolve=>setTimeout(resolve,10));if(callbacks.length) throw new Error('self-apply loop');
        owned.delete(cell);graph.removeCells([cell]);
      }
      if(applies!==15) throw new Error('missing layout application');
      checks.push({case:'real graph async prepare contract, special IDs, coalesced dirty/self-apply/next run',theme,result:'PASS',applies});
    } finally {graph.layoutManager.getLayout=getLayout;}
    const view = new mxGraphView(graph);view.scale=1;view.resetValidationState();
    const intersection = mxUtils.intersection, enabled = Graph.lineJumpsEnabled;let pairs=0;
    const state = (points) => {const value=new mxCellState(view,new mxCell(),{jumpStyle:'arc'});Object.assign(value,{x:0,y:0,width:100,height:100,absolutePoints:points});return value;};
    const start=performance.now();
    try {
      Graph.lineJumpsEnabled=true;mxUtils.intersection=function(){pairs++;return intersection.apply(this,arguments);};
      for(let i=0;i<2000;i++) {
        const edge=state([new mxPoint(0,i%2?100:0),new mxPoint(100,i%2?0:100)]);
        view.updateLineJumps(edge);view.validEdges.push(edge);
      }
      if(pairs>1000000 || !view.aideLineJumpsLimited) throw new Error('unbounded jumps');
      const stale=state([new mxPoint(0,0),new mxPoint(100,100)]);stale.routedPoints=[{}];
      if(!view.updateLineJumps(stale)||stale.routedPoints!==null) throw new Error('stale routed points');
      view.resetValidationState();
      const a=state([new mxPoint(0,50),new mxPoint(100,50)]), b=state([new mxPoint(50,0),new mxPoint(50,100)]);
      view.updateLineJumps(a);view.validEdges.push(a);view.updateLineJumps(b);
      if(!b.routedPoints.some(point=>point.type===1) || view.aideLineJumpsLimited) throw new Error('normal crossing jump lost');
      checks.push({case:'real view 2000 edges, conservative budget, clear stale jumps, reset and normal crossing',theme,result:'PASS',pairs,elapsedMs:performance.now()-start});
    } finally {mxUtils.intersection=intersection;Graph.lineJumpsEnabled=enabled;}
    let table;
    model.beginUpdate();
    try {
      table=graph.createTable(2,3,90,40,'Owned table');table.geometry.x=40;table.geometry.y=180;
      const cells=table.getChildAt(0).children;cells[0].geometry=null;cells[1].value='Column 2';cells[2].value='Column 3';
      graph.addCell(table,parent);new TableLayout(graph).execute(table);
    } finally {model.endUpdate();}
    const rows=table.children;
    for(const row of rows) for(const cell of row.children) if(cell.geometry) {
      for(const key of ['x','y','width','height']) if(!Number.isFinite(cell.geometry[key])) throw new Error('nonfinite table '+key);
    }
    const positions=new TableLayout(graph).getRowLayout(rows[0],table.geometry.width);
    if(positions.length!==3 || rows[0].children[0].geometry!==null) throw new Error('missing table column index');
    // Selecting this malformed table has a separate known handle bug (#137), next in the ordered queue.
    checks.push({case:'real table layout, missing first geometry, finite later columns and unchanged absent geometry',theme,result:'PASS',positions:Array.from(positions)});
    return checks;
  }, theme);
  assert.equal(checks.length,3);
  await frame.page().screenshot({path:path.join(out,theme+'-table-layout.png')});
  return checks;
};
