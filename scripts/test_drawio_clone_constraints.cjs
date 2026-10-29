'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
function main(mode='all') {
  for(const bundled of process.env.AIDE_DRAWIO_BUNDLE_ONLY?[true]:[false,true]) {
    const bundle=bundled?fs.readFileSync('internal/server/web/vendor/drawio/js/app.min.js','utf8'):null;
    if(mode==='all'||mode==='clone') {
      function Graph() {}const root={Graph};
      for(const name of ['createCellMapping','updateCustomLink','updateCustomLinkActions','updateCustomLinkAction']) {
        const source=bundle||fs.readFileSync('internal/server/web/vendor/drawio/js/'+(name==='createCellMapping'?'grapheditor/Graph.js':'diagramly/Editor.js'),'utf8');
        vm.runInNewContext(assignment(source,'Graph.prototype.'+name),root);
      }
      for(const id of ['constructor','normal','__proto__','toString','hasOwnProperty']) {
        for (const supplied of [undefined,{}]) {
          const graph=new Graph(),map=graph.createCellMapping({object:{getId:()=>('clone-'+id)},empty:{getId:()=>null}},{object:id,empty:'empty'},supplied);
          if(supplied)assert.equal(map,supplied,'caller-supplied mapping identity preserved');else assert.equal(Object.getPrototypeOf(map),null);
          assert.equal(map[id],'clone-'+id,'must map special source ID '+id);
          const fields={cells:[id,'*','valueOf','empty'],excludeCells:[id],layers:[id]};
          const result=JSON.parse(graph.updateCustomLink(map,'data:action/json,'+JSON.stringify({actions:[{toggle:fields}]})).slice(17));
          assert.deepEqual(result.actions[0].toggle,{cells:['clone-'+id,'*','valueOf'],excludeCells:['clone-'+id],layers:['clone-'+id]});
        }
      }
    }
    if(mode==='all'||mode==='leave') {
      function Handler() {}let destroyed=0, redraw=0;
      const listeners=new Set(),focus={cell:{}},model={removeListener(){},isVertex:()=>true};
      const graph={container:{},model,view:{getState:()=>focus,removeListener(){}},removeListener(){},getModel:()=>model};
      const root={mxConstraintHandler:Handler,mxRectangle:function(){},mxUtils:{bind:(obj,fn)=>fn.bind(obj)},
        mxEvent:{addListener:(_node,event,fn)=>{assert.equal(event,'mouseleave');listeners.add(fn);},removeListener:(_node,event,fn)=>listeners.delete(fn)}};
      const source=bundle||fs.readFileSync('internal/server/web/vendor/drawio/mxgraph/src/handler/mxConstraintHandler.js','utf8');
      for(const name of ['update','reset','destroy'])vm.runInNewContext(assignment(source,'mxConstraintHandler.prototype.'+name),root);
      const handler=Object.assign(new Handler(),{graph,currentFocus:focus,isEnabled:()=>true,isEventIgnored:()=>false,getTolerance:()=>0,
        isKeepFocusEvent:()=>true,getCellForEvent:()=>focus.cell,destroyFocusHighlight(){},resetHandler:()=>redraw++});
      const event={getGraphX:()=>0,getGraphY:()=>0};handler.update(event,true);handler.update(event,true);assert.equal(listeners.size,1);
      handler.focusIcons=[{destroy:()=>destroyed++}];handler.focusHighlight={destroy:()=>destroyed++};
      for(const callback of listeners)callback();
      assert.equal(handler.currentFocus,null,'mouseleave must clear focus even when its cell still exists');
      assert.equal(handler.focusIcons,null);assert.equal(handler.focusHighlight,null);assert.equal(destroyed,2);assert.equal(redraw,0);
      handler.destroy();assert.equal(listeners.size,0,'destroy must remove exactly the registered listener');
    }
  }
  console.log('PASS draw.io '+mode+' (source and actual bundled functions)');
}
if(require.main===module)try{main(process.argv[2]||'all');}catch(error){console.error(error.message);process.exitCode=1;}
