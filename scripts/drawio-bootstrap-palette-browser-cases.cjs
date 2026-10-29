'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs'),{methods}=require('./drawio-bootstrap-palette-review.cjs');
module.exports=async function(frame,out,theme){
 const checks=[];
 for(const variant of ['source','app.min.js','integrate.min.js']){
  const code=assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+(variant==='source'?methods[0][0]:variant),'utf8'),methods[0][1]);
  const observed=await frame.evaluate(code=>{
   function CapturedSidebar(){}CapturedSidebar.prototype=Object.create(Sidebar.prototype);Function('Sidebar',code)(CapturedSidebar);
   const sb=new CapturedSidebar(),entries=[],mismatches=[];sb.taglist=null;sb.setCurrentSearchEntryLibrary=()=>{};sb.addPalette=()=>{};
   sb.addEntry=(tags,fn)=>{entries.push({tags,fn});return fn;};sb.createVertexTemplateFromCells=(cells,width,height,title)=>({cells,width,height,title});sb.addBootstrapPalette();
   const snap=c=>({value:c.value,style:c.style,geometry:{x:c.geometry.x,y:c.geometry.y,width:c.geometry.width,height:c.geometry.height,relative:c.geometry.relative,offset:c.geometry.offset?{x:c.geometry.offset.x,y:c.geometry.offset.y}:null},children:(c.children||[]).map(snap)});
   const texts=c=>[c.value,...(c.children||[]).flatMap(texts)].filter(v=>typeof v==='string');
   for(const e of entries){e.template=e.fn();e.snapshot=e.template.cells.map(snap);e.text=e.template.cells.flatMap(texts);}
   const cards=entries.filter(e=>e.text.some(v=>/Last (?:udated 3 mind|updated 3 mins) ago|paragraphy|short paragraph of text/.test(v)));
   const all=entries.flatMap(e=>e.text),badDate=all.filter(v=>v.includes('Last udated 3 mind ago')).length,badParagraph=all.filter(v=>v.includes('paragraphy')).length;
   if(badDate||badParagraph)mismatches.push('199 inserted copy has '+badDate+' bad timestamps/'+badParagraph+' paragraphy');
   const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
   const renderedCards=[];
   for(const [i,e]of cards.entries()){
    const cells=graph.importCells(e.template.cells,80+i%2*1300,80+Math.floor(i/2)*600,graph.getDefaultParent());
    const actual=cells.flatMap(c=>graph.model.getDescendants(c)).flatMap(c=>texts(c).slice(0,1));
    for(const c of cells.flatMap(c=>graph.model.getDescendants(c)))if(!graph.view.getState(c)?.shape)mismatches.push('199 missing rendered card cell');
    const renderedText=cells.flatMap(c=>graph.model.getDescendants(c)).map(c=>graph.view.getState(c)?.text?.node?.textContent||'').join('\n');
    if(/Last udated 3 mind ago|paragraphy/.test(renderedText))mismatches.push('199 actual rendered HTML contains misspelling');
    renderedCards.push({title:e.template.title,text:actual,renderedText});
   }
   const holder=document.createElement('div');holder.id='owned-bootstrap-palette';holder.style.cssText='position:fixed;left:8px;top:125px;z-index:10000;display:flex;background:#ddd;padding:4px;gap:5px;';document.body.appendChild(holder);
   const actual=Object.create(ownedUi.sidebar);actual.virtualThumbs=false;actual.container=holder;
   const sources=[],original=Sidebar.prototype.createDragSource;
   actual.createDragSource=function(elt,drop,preview,cells,bounds){const ds=original.apply(this,arguments);sources.push({elt,drop,preview,cells,bounds,ds});return ds;};
   const compound=[],singles=[];
   for(const name of ['Dropup button','Dropright button','Dropleft button']){
    const found=entries.filter(e=>e.template.title===name),multiple=found.filter(e=>e.template.cells.length===2),single=found.filter(e=>e.template.cells.length===1);
    if(multiple.length!==1||single.length!==1)throw Error('directional factory counts '+name);
    const e=multiple[0],t=e.template,cells=t.cells;
    const minX=Math.min(...cells.map(c=>c.geometry.x)),minY=Math.min(...cells.map(c=>c.geometry.y)),right=Math.max(...cells.map(c=>c.geometry.x+c.geometry.width)),bottom=Math.max(...cells.map(c=>c.geometry.y+c.geometry.height));
    const elt=actual.createVertexTemplateFromCells(cells,t.width,t.height,t.title,false,false,true,false);elt.id='owned-bootstrap-item-'+compound.length;holder.appendChild(elt);
    const source=sources.at(-1);if(source.bounds.width<right||source.bounds.height<bottom||minX<0||minY<0)mismatches.push('200 '+name+' declared '+t.width+'x'+t.height+' vs extent '+right+'x'+bottom);
    if(parseFloat(source.preview.style.width)!==t.width||parseFloat(source.preview.style.height)!==t.height)mismatches.push('200 preview not from declared dimensions');
    graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
    source.drop(graph,new MouseEvent('mouseup'),null,100,100,true);
    const inserted=graph.getSelectionCells();if(inserted.length!==2)mismatches.push('200 normal drop missing roots '+name);
    const a=graph.insertVertex(graph.getDefaultParent(),null,'from',600,100,60,40),b=graph.insertVertex(graph.getDefaultParent(),null,'to',1000,100,60,40),edge=graph.insertEdge(graph.getDefaultParent(),null,'',a,b);
    const splitSupported=graph.isSplitTarget(edge,source.cells,new MouseEvent('mouseup'));
    compound.push({title:t.title,width:t.width,height:t.height,extent:{minX,minY,right,bottom},snapshot:e.snapshot,preview:{width:parseFloat(source.preview.style.width),height:parseFloat(source.preview.style.height)},insertedRoots:inserted.length,splitSupported});
    const one=single[0];singles.push({title:one.template.title,width:one.template.width,height:one.template.height,snapshot:one.snapshot});
   }
   graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();graph.container.setAttribute('data-owned-bootstrap-graph','1');
   window.ownedBootstrap={holder,sources,graph,cards};
   return {mismatches,materializedFactories:entries.length,cards:cards.map(e=>({title:e.template.title,width:e.template.width,height:e.template.height,tags:e.tags,snapshot:e.snapshot})),badDate,badParagraph,renderedCards,compound,singles};
  },code);
  try{
   // Real pointer drag through the actual palette item's installed drag source.
   // A narrow editor can have no usable canvas; its real drop/preview path above
   // is still checked, while physical mouse drags are bounded to desktop.
   observed.pointerDrags=[];
   if(theme==='light'){
    for(let i=0;i<3;i++){
     await frame.evaluate(()=>{const g=ownedBootstrap.graph;g.selectAll();g.removeCells(g.getSelectionCells());g.clearSelection();});
     const item=await frame.locator('#owned-bootstrap-item-'+i).boundingBox(),canvas=await frame.locator('[data-owned-bootstrap-graph]').boundingBox();assert.ok(item&&canvas&&canvas.width>200);
     await frame.page().mouse.move(item.x+item.width/2,item.y+item.height/2);await frame.page().mouse.down();
     await frame.page().mouse.move(canvas.x+Math.min(260,canvas.width*.55),canvas.y+Math.min(210,canvas.height*.5),{steps:15});
     const preview=await frame.evaluate(i=>{const {graph,sources}=ownedBootstrap,s=sources[i],p=s.ds.previewElement;return {active:!!s.ds.dragElement,preview:p?{width:parseFloat(p.style.width),height:parseFloat(p.style.height)}:null,scale:graph.view.scale};},i);
     await frame.page().mouse.up();
     const dropped=await frame.evaluate(()=>{const g=ownedBootstrap.graph,c=g.getSelectionCells();return {roots:c.length,children:c.map(x=>x.getChildCount()),values:c.map(x=>x.value)};});
     if(!preview.active||!preview.preview||dropped.roots!==2)observed.mismatches.push('200 real pointer drag/drop failed '+i);
     if(preview.preview&&(Math.abs(preview.preview.width-observed.compound[i].width*preview.scale)>.1||Math.abs(preview.preview.height-observed.compound[i].height*preview.scale)>.1))observed.mismatches.push('200 live preview scale mismatch '+i);
     observed.pointerDrags.push({title:observed.compound[i].title,...preview,dropped});
    }
   }
   await frame.evaluate(()=>{ownedBootstrap.holder.style.display='none';ownedUi.actions.get('fitWindow').funct();});
   await frame.page().screenshot({path:path.join(out,theme+'-bootstrap-'+variant+'.png')});
   await frame.evaluate(()=>{const {graph,cards}=ownedBootstrap;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();const examples=[cards[0],cards.find(e=>e.text.some(v=>/short paragraph/.test(v)))];for(const [i,e]of examples.entries())graph.importCells(e.template.cells,80+i*1300,80,graph.getDefaultParent());ownedUi.actions.get('fitWindow').funct();});
   await frame.page().screenshot({path:path.join(out,theme+'-bootstrap-cards-'+variant+'.png')});
   fs.writeFileSync(path.join(out,theme+'-bootstrap-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');
   if(observed.mismatches.length)console.log('Bootstrap diagnostics '+JSON.stringify({variant,theme,mismatches:observed.mismatches}));
   assert.deepEqual(observed.mismatches,[],variant+' actual card copy/directional preview and drop bounds');
   checks.push({case:'199 actual card factory/cell copy/render;200 actual createItem preview/drop and desktop pointer drag;single controls',variant,theme,result:'PASS',cards:observed.cards.length,compound:observed.compound.length,pointerDrags:observed.pointerDrags.length});
  }finally{await frame.page().mouse.up();await frame.evaluate(()=>{if(window.ownedBootstrap){for(const s of ownedBootstrap.sources)s.ds.reset();mxEvent.release(ownedBootstrap.holder);ownedBootstrap.holder.remove();delete window.ownedBootstrap;}});}
 }
 return checks;
};
