'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
// These assignments have no regexp literals; skip quoted strings and comments while balancing braces.
function assignment(source, name) {
  const pattern = new RegExp(name.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'\\s*=\\s*function\\s*\\(');
  const start = source.search(pattern);assert.ok(start >= 0,name);
  const begin = source.indexOf('{',start);let depth = 0, quote = '', comment = '';
  for (let i = begin; i < source.length; i++) {
    const c = source[i], next = source[i+1];
    if (comment === '//') {if(c==='\n') comment='';continue;}
    if (comment === '/*') {if(c==='*'&&next==='/'){comment='';i++;}continue;}
    if (quote) {if(c==='\\') i++;else if(c===quote) quote='';continue;}
    if(c==='/'&&(next==='/'||next==='*')){comment=c+next;i++;continue;}
    if(c==='"'||c==="'"){quote=c;continue;}
    if(c==='{')depth++;else if(c==='}'&&--depth===0)return source.slice(start,i+1)+';';
  }
  throw new Error('Unclosed '+name);
}
function runtime(bundled) {
  const source = fs.readFileSync('internal/server/web/vendor/drawio/js/'+(bundled?'app.min.js':'grapheditor/Graph.js'),'utf8');
  const ticks = [];let pairs = 0;
  function Graph() {}Graph.lineJumpsEnabled=true;
  function View() {}View.prototype.resetValidationState=function(){this.validEdges=[];};
  function TableLayout(graph){this.graph=graph;}
  const root = {Graph,mxGraphView:View,TableLayout,mxCompositeLayout:function(){},mxEvent:{END_UPDATE:'end'},
    mxPoint:function(x,y){this.x=x;this.y=y;},console,
    mxRectangle:function(x,y,width,height){Object.assign(this,{x,y,width,height});},
    window:{setTimeout:fn=>ticks.push(fn)},mxUtils:{indexOf:(a,b)=>a.indexOf(b),bind:(obj,fn)=>fn.bind(obj),
      getValue:(obj,key,fallback)=>obj[key]??fallback,intersects:()=>true,ptSegDistSq:()=>2,
      intersection:()=>{pairs++;return null;},ptLineDist:()=>2}};
  for(const name of ['Graph.prototype.scheduleAsyncLayout','Graph.prototype.runAsyncLayout',
    'TableLayout.prototype.getSize','TableLayout.prototype.getRowLayout','TableLayout.prototype.layoutRow',
    'mxGraphView.prototype.updateLineJumps']) vm.runInNewContext(assignment(source,name),root);
  if(!process.env.AIDE_DRAWIO_SKIP_LAYOUT_FIXES && fs.existsSync('internal/server/web/drawio-layout-fixes.js')) require('../internal/server/web/drawio-layout-fixes.js')(root).install();
  return {root,get pairs(){return pairs;},flush(){let count=0;while(ticks.length){assert.ok(++count<100);ticks.shift()();}}};
}
function main(mode='all') {
  for(const bundled of process.env.AIDE_DRAWIO_BUNDLE_ONLY ? [true] : [false,true]) {
    if(mode==='all'||mode==='ids') {
      for(const id of ['ordinary','constructor','toString','hasOwnProperty','__proto__']) {
        const f=runtime(bundled),g=new f.root.Graph(),cell={id,parent:null},callbacks=[];let applies=0;
        const model={contains:()=>true,beginUpdate(){},endUpdate(){}};
        const layout={prepare(_cell,done){callbacks.push(done);}};
        g.getModel=()=>model;g.isCellCollapsed=()=>false;g.isAsyncLayout=()=>true;g.layoutManager={getLayout:()=>layout};
        g.scheduleAsyncLayout(cell);f.flush();assert.equal(callbacks.length,1,id+' initial prepare');
        g.scheduleAsyncLayout(cell);g.scheduleAsyncLayout(cell);
        callbacks.shift()(null,()=>{applies++;g.scheduleAsyncLayout(cell);});f.flush();
        assert.equal(callbacks.length,1,id+' dirty run must execute once');
        callbacks.shift()(null,()=>{applies++;g.scheduleAsyncLayout(cell);});f.flush();
        assert.equal(callbacks.length,0,id+' self apply must not loop');
        g.scheduleAsyncLayout(cell);f.flush();assert.equal(callbacks.length,1,id+' later run must not stall');
        callbacks.shift()(null,()=>applies++);assert.equal(applies,3);
      }
    }
    if(mode==='all'||mode==='jumps') {
      const f=runtime(bundled),view=new f.root.mxGraphView();view.scale=1;view.resetValidationState();
      for(let i=0;i<2000;i++) {
        const state={absolutePoints:[{x:0,y:0},{x:100,y:100}],style:{jumpStyle:'arc'}};
        view.updateLineJumps(state);view.validEdges.push(state);
      }
      assert.ok(f.pairs<=1000000,'line-jump intersection work must remain bounded; actual='+f.pairs);
      assert.equal(view.aideLineJumpsLimited,true);
      const stale={absolutePoints:Array.from({length:129},()=>({x:0,y:0})),style:{jumpStyle:'arc'},routedPoints:[{}]};
      assert.equal(view.updateLineJumps(stale),true);assert.equal(stale.routedPoints,null);
      view.resetValidationState();assert.equal(view.aideLineJumpsLimited,false);
      const before=f.pairs;view.updateLineJumps(stale);assert.equal(view.aideLineJumpsLimited,true);assert.equal(f.pairs,before,'point limit rejects before computing intersections');
      view.resetValidationState();view.validEdges.push({absolutePoints:Array.from({length:20000},()=>({x:0,y:0})),style:{}});
      view.updateLineJumps({absolutePoints:[{x:0,y:0},{x:10,y:10}],style:{jumpStyle:'arc'}});
      assert.equal(f.pairs,before,'large prior polyline rejects before intersection loops');
      view.resetValidationState();
      const small={absolutePoints:[{x:0,y:0},{x:10,y:10}],style:{jumpStyle:'arc'}};
      view.updateLineJumps(small);assert.equal(small.routedPoints.length,2);
    }
    if(mode==='all'||mode==='table') {
      function geo(width) {return {x:0,y:0,width,height:20,clone(){return {...this};}};}
      for(const missing of [[0],[1],[2],[],[0,1,2]]) {
        const f=runtime(bundled),cells=[{geo:geo(20)},{geo:geo(30)},{geo:geo(50)}];for(const i of missing)cells[i].geo=null;
        const model={getChildCells:()=>cells,setGeometry:(cell,value)=>{cell.geo=value;}};
        const graph={model,getModel:()=>model,getActualStartSize:()=>({x:10,y:4,width:10,height:2}),getCellGeometry:c=>c.geo};
        const layout=new f.root.TableLayout(graph);layout.isVertexIgnored=()=>false;
        const positions=layout.getRowLayout({},220);assert.equal(positions.length,3,'missing geometry must preserve column boundary index');
        if(!missing.length)assert.deepEqual(Array.from(positions),[50,110,210],'normal column geometry unchanged');
        layout.layoutRow({},positions,30,220);
        for(const c of cells) if(c.geo) for(const key of ['x','y','width','height']) assert.ok(Number.isFinite(c.geo[key]),key);
        for(const i of missing)assert.equal(cells[i].geo,null);
      }
    }
  }
  console.log('PASS draw.io '+mode+' (unminified and actual bundled functions)');
}
module.exports={assignment};
if(require.main===module)try{main(process.argv[2]||'all');}catch(error){console.error(error.message);process.exitCode=1;}
