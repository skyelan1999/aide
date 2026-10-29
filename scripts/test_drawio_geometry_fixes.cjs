'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
function member(source,owner,name) {
  const pattern=new RegExp('\\b'+name+'\\s*:\\s*function\\s*\\(');
  assert.ok(pattern.test(source),name);
  return assignment(source.replace(pattern,owner+'.'+name+'=function('),owner+'.'+name);
}
function main(mode='all') {
  const copies=process.env.AIDE_DRAWIO_COPY?[process.env.AIDE_DRAWIO_COPY]:['source','app','core'];
  for(const copy of copies) {
    const bundle=copy==='source'?null:fs.readFileSync('internal/server/web/vendor/drawio/'+(copy==='app'?'js/app.min.js':'mxgraph/mxClient.js'),'utf8');
    if(mode==='all'||mode==='rotation') {
      function Handler() {}const root={mxVertexHandler:Handler,mxPoint:function(x,y){this.x=x;this.y=y;}};
      const source=bundle||fs.readFileSync('internal/server/web/vendor/drawio/mxgraph/src/handler/mxVertexHandler.js','utf8');
      vm.runInNewContext(assignment(source,'mxVertexHandler.prototype.rotateVertex'),root);
      for(const snap of [15,5,1]) for(const existing of [false,true]) {
        if(existing)root.raster='owned sentinel';else delete root.raster;
        const handler=Object.assign(new Handler(),{state:{x:0,y:0,width:100,height:100,getCenterX:()=>50,getCenterY:()=>50},
          startAngle:0,startDist:100-(snap===15?0:snap===5?10:30),rotationRaster:true,
          graph:{isGridEnabledEvent:()=>true},selectionBorder:{redraw(){}}});
        const angle=37*Math.PI/180,point={x:50+100*Math.sin(angle),y:50-100*Math.cos(angle)};
        handler.rotateVertex({getGraphX:()=>point.x,getGraphY:()=>point.y,getEvent:()=>({})});
        assert.equal(handler.currentAlpha,Math.round(37/snap)*snap);
        if(existing)assert.equal(root.raster,'owned sentinel','rotation must preserve page-global raster');
        else assert.equal(Object.hasOwn(root,'raster'),false,'rotation must not create page-global raster');
      }
    }
    if(mode==='all'||mode==='loop') {
      const stop=new Error('terminal-routing boundary'),edgeStyle={},root={mxEdgeStyle:edgeStyle,mxConstants:{DIRECTION_MASK_ALL:15},mxUtils:{getPortConstraints(){throw stop;}}};
      const source=bundle||fs.readFileSync('internal/server/web/vendor/drawio/mxgraph/src/view/mxEdgeStyle.js','utf8');
      edgeStyle.scalePointArray=(pts,scale)=>pts.map(p=>({x:p.x/scale,y:p.y/scale}));
      edgeStyle.scaleCellState=(state,scale)=>state?{...state,x:state.x/scale,y:state.y/scale,width:state.width/scale,height:state.height/scale}:null;
      vm.runInNewContext(member(source,'mxEdgeStyle','OrthConnector'),root);
      for(const scale of [1,2]) for(const [a,b,distance,expected] of [[20,80,120,true],[80,20,120,true],[20,80,160,false],[20,20,120,false]]) {
        let fallback=0;edgeStyle.getJettySize=(_state,isSource)=>isSource?a:b;edgeStyle.SegmentConnector=()=>fallback++;
        const terminal={x:0,y:0,width:100,height:100,style:{}},state={view:{graph:{},scale},absolutePoints:[{x:0,y:0},{x:distance*scale,y:0}]};
        try{edgeStyle.OrthConnector(state,terminal,terminal,null,[]);}catch(error){if(error!==stop)throw error;}
        assert.equal(Boolean(fallback),expected,'self-loop must use shared max jetty before cloned states lose identity');
        fallback=0;
        try{edgeStyle.OrthConnector(state,terminal,{...terminal},null,[]);}catch(error){if(error!==stop)throw error;}
        assert.equal(fallback,0,'distinct terminals retain independent buffers');
      }
    }
    if(mode==='all'||mode==='arc') {
      function Canvas(){this.lastX=5;this.lastY=7;this.ops=[];this.lineOp='L';this.curveOp='C';}
      Canvas.prototype.addOp=function(op,...args){this.ops.push([op,...args]);this.lastX=args.at(-2);this.lastY=args.at(-1);};
      const root={mxUtils:{},mxAbstractCanvas2D:Canvas};
      const utils=bundle||fs.readFileSync('internal/server/web/vendor/drawio/mxgraph/src/util/mxUtils.js','utf8');
      const canvas=bundle||fs.readFileSync('internal/server/web/vendor/drawio/mxgraph/src/util/mxAbstractCanvas2D.js','utf8');
      vm.runInNewContext(member(utils,'mxUtils','arcToCurves'),root);
      for(const name of ['arcTo','lineTo','curveTo'])vm.runInNewContext(assignment(canvas,'mxAbstractCanvas2D.prototype.'+name),root);
      for(const [rx,ry] of [[0,10],[10,0],[0,0],[-0,10]]) {
        const curves=root.mxUtils.arcToCurves(5,7,rx,ry,0,false,true,40,50);
        assert.deepEqual(Array.from(curves||[]),[5,7,40,50,40,50],'utility returns finite straight cubic for degenerate arc');
        const c=new Canvas();c.arcTo(rx,ry,0,false,true,40,50);c.lineTo(60,70);
        assert.deepEqual(c.ops,[['L',40,50],['L',60,70]],'canvas draws line and advances endpoint');
        const same=new Canvas();same.arcTo(rx,ry,0,false,true,5,7);assert.equal(same.ops.length,0,'identical endpoints remain no-op');
      }
      const normal=new Canvas();normal.arcTo(20,30,0,false,true,40,50);
      assert.ok(normal.ops.length>0 && normal.ops.every(op=>op[0]==='C'&&op.slice(1).every(Number.isFinite)));
      assert.ok(Math.abs(normal.lastX-40)<1e-8&&Math.abs(normal.lastY-50)<1e-8);
    }
  }
  console.log('PASS draw.io '+mode+' geometry (source, production app and standalone mxClient)');
}
module.exports={member};
if(require.main===module)try{main(process.argv[2]||'all');}catch(error){console.error(error.message);process.exitCode=1;}
