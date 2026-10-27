'use strict';
const assert=require('node:assert/strict'),vm=require('node:vm');
const {files,code}=require('./drawio-freehand-source.cjs');
function runtime(file,perfect,smoothing=1){
 const state={previewMax:0,previewWork:0,maxCommands:0,committed:0,cells:[],xml:''};
 class Element{
  constructor(name){this.nodeName=name;this.attrs={};this.children=[];this.parentNode=null;}
  setAttribute(key,value){this.attrs[key]=String(value);if(key==='d')state.maxCommands=Math.max(state.maxCommands,(String(value).match(/ L/g)||[]).length+1);}
  getAttribute(key){return this.attrs[key]??null;}removeAttribute(key){delete this.attrs[key];}
  appendChild(node){this.children.push(node);node.parentNode=this;return node;}
  removeChild(node){this.children.splice(this.children.indexOf(node),1);node.parentNode=null;return node;}
  cloneNode(){const node=new Element(this.nodeName);node.attrs={...this.attrs};return node;}
 }
 const svg=new Element('svg');let mouse;
 const graph={view:{canvas:{ownerSVGElement:svg},scale:1,translate:{x:0,y:0}},container:{classList:{add(){},remove(){}}},currentVertexStyle:{strokeWidth:1},shapeForegroundColor:'#1989d1',tolerance:2,addListener(){},addMouseListener(listener){mouse=listener;},getRubberband:()=>({setEnabled(){}}),graphHandler:{setSelectEnabled(){},setMoveEnabled(){}},panningHandler:{setPinchEnabled(){}},fireEvent(){},isEnabled:()=>true,isCellLocked:()=>false,getDefaultParent:()=>null,getCurrentCellStyle:()=>({}),model:{beginUpdate(){},endUpdate(){}},addCell(cell){state.cells.push(cell);return cell;}};
 const context={document:{createElementNS:(ns,name)=>new Element(name)},mxCell:class{constructor(value,geometry,style){Object.assign(this,{value,geometry,style});}},mxGeometry:class{constructor(x,y,width,height){Object.assign(this,{x,y,width,height});}},mxEventObject:class{},mxConstants:{STYLE_STROKEWIDTH:'strokeWidth',STYLE_STROKECOLOR:'strokeColor',STYLE_DASHED:'dashed',STYLE_DASH_PATTERN:'dashPattern',STYLE_SHAPE:'shape'},mxEvent:{ESCAPE:'escape',FIRE_MOUSE_EVENT:'fire',GESTURE:'gesture',consume(){},isPopupTrigger:()=>false,isMiddleMouseButton:()=>false,isMultiTouchEvent:()=>false,getClientX:e=>e.clientX,getClientY:e=>e.clientY},mxUtils:{bind:(self,fn)=>fn.bind(self),getValue:(object,key,value)=>object[key]??value,convertPoint:(container,x,y)=>({x,y})},Graph:{compress:xml=>{state.xml=xml;return 'owned';}},PerfectFreehand:{getSvgPathFromStroke(points){state.previewMax=Math.max(state.previewMax,points.length);state.previewWork+=points.length;return 'M'+points.length;},getStroke(points){state.committed=points.length;return points.map(point=>point.slice());}}};
 vm.runInNewContext(code(file),context);const freehand=new context.mxFreehand(graph);assert.equal(freehand.getSmoothing(),4,'original mild smoothing default retained');freehand.setPerfectFreehandMode(perfect);freehand.setSmoothing(smoothing);freehand.setAutoScroll(false);freehand.setAutoClose(false);freehand.startDrawing();
 function event(i){return {getEvent:()=>({clientX:i,clientY:20+Math.sin(i/20)*10}),consume(){}};}
 return {state,freehand,svg,draw(n){mouse.mouseDown(graph,event(0));for(let i=1;i<=n;i++)mouse.mouseMove(graph,event(i));const during=svg.children.length;mouse.mouseUp(graph,event(n));freehand.stopDrawing();return {during,remaining:svg.children.length};}};
}
function main(){
 const failures=[];
 for(const file of files)for(const perfect of [true,false])for(const smoothing of [1,4,8,12,16,20]){
  const r=runtime(file,perfect,smoothing),lifecycle=r.draw(1500);const measured=perfect?r.state.previewMax:r.state.maxCommands;
  console.log(JSON.stringify({file,perfect,smoothing,maxPreviewPoints:measured,previewWork:r.state.previewWork,committedPoints:r.state.committed,vertices:r.state.cells.length,remaining:lifecycle.remaining}));
  if(measured>(smoothing===1?512:520))failures.push({file,perfect,smoothing,maxPreviewPoints:measured});
  assert.equal(r.state.cells.length,1,'one complete stroke becomes one vertex');assert.equal(lifecycle.remaining,0,'completed preview nodes removed');assert.equal((r.state.xml.match(/<line /g)||[]).length,1500,'all committed points survive preview optimization');if(perfect)assert.equal(r.state.committed,1501);
 }
 assert.deepEqual(failures,[],'each pointer update must have a bounded preview history');
 console.log('PASS source/app/integrate bounded preview work, complete sample commit and cleanup in both modes');
}
module.exports={runtime};if(require.main===module)main();
