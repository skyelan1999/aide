'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
function source(copy,file) {return fs.readFileSync('internal/server/web/vendor/drawio/'+(copy==='source'?'mxgraph/src/'+file:copy==='app'?'js/app.min.js':'mxgraph/mxClient.js'),'utf8');}
function main(mode='all') {
  for(const copy of process.env.AIDE_DRAWIO_COPY?[process.env.AIDE_DRAWIO_COPY]:['source','app','core']) {
    if(mode==='all'||mode==='menus') {
      function Menu() {}function Graph() {}const released=new Set(),attached=new Set();
      const body={removeChild(node){attached.delete(node);node.parentNode=null;}};
      function node(open) {const value={childNodes:[]};if(open){value.parentNode=body;attached.add(value);}return value;}
      const nested={div:node(true),tbody:{firstChild:null}},open={div:node(true),tbody:{firstChild:nested},activeRow:nested};
      const closed={div:node(false),tbody:{firstChild:null}};open.nextSibling=closed;
      const menu=Object.assign(new Menu(),{div:node(true),tbody:{firstChild:open},activeRow:open,eventReceiver:open});
      const root={mxPopupMenu:Menu,mxGraph:Graph,mxEvent:{release:div=>released.add(div)}};
      vm.runInNewContext(assignment(source(copy,'util/mxPopupMenu.js'),'mxPopupMenu.prototype.destroy'),root);
      vm.runInNewContext(assignment(source(copy,'view/mxGraph.js'),'mxGraph.prototype.destroy'),root);
      const graph=Object.assign(new Graph(),{popupMenuHandler:menu,container:{}}),divs=[menu.div,open.div,nested.div,closed.div];
      graph.destroy();assert.equal(attached.size,0,'graph teardown removes all body-attached submenu nodes');
      assert.ok(divs.every(div=>released.has(div)),'closed and open submenus release listeners outside root subtree');
      assert.equal(menu.eventReceiver,null);assert.equal(menu.activeRow,null);graph.destroy();menu.destroy();
    }
    if(mode==='all'||mode==='panning') {
      for(const throughGraph of [false,true]) for(const staleInactiveFlag of [false,true]) {
        function Graph() {}const timers=new Map(),listeners=new Set(),mouse=new Set();let id=0,pans=0;
        const root={mxPanningManager:function(){},mxGraph:Graph,document:{},window:{setInterval(fn){timers.set(++id,fn);return id;},clearInterval:id=>timers.delete(id)},
          mxEventObject:function(){},mxEvent:{PAN:'pan',addListener:(_node,_name,fn)=>listeners.add(fn),removeListener:(_node,_name,fn)=>listeners.delete(fn)},
          mxUtils:{bind:(obj,fn)=>fn.bind(obj),hasScrollbars:()=>false}};
        let text=source(copy,'util/mxPanningManager.js').replace(/function mxPanningManager\s*\(/,'mxPanningManager=function(');
        vm.runInNewContext(assignment(text,'mxPanningManager'),root);
        Object.assign(root.mxPanningManager.prototype,{damper:1/6,delay:10,handleMouseOut:true,border:0});
        vm.runInNewContext(assignment(source(copy,'view/mxGraph.js'),'mxGraph.prototype.destroy'),root);
        const graph=Object.assign(new Graph(),{container:{scrollLeft:0,scrollTop:0,clientWidth:100,clientHeight:100},panDx:0,panDy:0,
          view:{translate:{x:0,y:0},scale:1,setTranslate(){},destroy(){}},addMouseListener:fn=>mouse.add(fn),removeMouseListener:fn=>mouse.delete(fn),
          panGraph(x,y){pans++;this.panDx=x;this.panDy=y;}});
        const manager=new root.mxPanningManager(graph);graph.panningManager=manager;manager.panTo(200,200);
        assert.equal(timers.size,1);for(const fn of timers.values())fn();assert.ok(pans>0,'positive path actually auto-pans');
        if(staleInactiveFlag)manager.active=false;
        if(throughGraph)graph.destroy();else manager.destroy();
        assert.equal(timers.size,0,'teardown must clear active interval');assert.equal(listeners.size,0);assert.equal(mouse.size,0);assert.equal(manager.thread,null);
        const after=pans;for(const fn of timers.values())fn();assert.equal(pans,after);
        if(throughGraph){assert.equal(graph.panningManager,null);graph.destroy();}else manager.destroy();
      }
    }
    if(mode==='all'||mode==='alpha') {
      function Stencil() {}function Canvas() {this.state={alpha:.8,fillAlpha:1,strokeAlpha:1};}
      const root={mxStencil:Stencil,mxAbstractCanvas2D:Canvas};
      vm.runInNewContext(assignment(source(copy,'shape/mxStencil.js'),'mxStencil.prototype.drawNode'),root);
      for(const name of ['setAlpha','setFillAlpha','setStrokeAlpha'])vm.runInNewContext(assignment(source(copy,'util/mxAbstractCanvas2D.js'),'mxAbstractCanvas2D.prototype.'+name),root);
      for(const [name,key] of [['fillalpha','fillAlpha'],['strokealpha','strokeAlpha'],['alpha','alpha']]) {
        const canvas=new Canvas(),expected={...canvas.state,[key]:'0.25'};
        new Stencil().drawNode(canvas,{}, {nodeName:name,getAttribute:()=> '0.25'},{x:0,y:0,width:1,height:1},false,true);
        assert.deepEqual(canvas.state,expected,'stencil '+name+' changes only its designated alpha');
      }
    }
  }
  console.log('PASS draw.io '+mode+' lifecycle/alpha (source, app and standalone mxClient)');
}
if(require.main===module)try{main(process.argv[2]||'all');}catch(error){console.error(error.message);process.exitCode=1;}
