'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs'),{methods,initializers,targets}=require('./drawio-state-search-review.cjs');
module.exports=async function(frame,out,theme){
 const checks=[];
 for(const variant of ['source','app.min.js','integrate.min.js']){
  const code=[...methods,...initializers].map(([file,name])=>assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+(variant==='source'?file:variant),'utf8'),name));
  const observed=await frame.evaluate(({code,names,targets})=>{
   function CapturedSidebar(){}CapturedSidebar.prototype=Object.create(Sidebar.prototype);
   Function('Sidebar',code.join('\n'))(CapturedSidebar);
   const sb=new CapturedSidebar(),entries=[],mismatches=[];sb.taglist=Object.create(null);
   for(const name of Object.getOwnPropertyNames(Sidebar.prototype))if(name.startsWith('addArchimate3')&&name.endsWith('Palette')&&!names.includes(name)&&name!=='addArchimate3Palette')sb[name]=()=>{};
   sb.setCurrentSearchEntryLibrary=()=>{};sb.addPalette=()=>{};sb.addPaletteFunctions=()=>{};
   const factory=Sidebar.prototype.createVertexTemplateEntry;let family;
   sb.createVertexTemplateEntry=function(style,width,height,value,title,_a,_b,tags){
    const fn=factory.apply(this,arguments),entry={family,style,width,height,value,title,tags};entries.push(entry);fn.ownedReview=entry;return fn;
   };
   for(const [group,method]of [['android','addAndroidPalette'],['archimate2','addArchiMatePalette'],['archimate3','addArchimate3Palette'],['arrows2','addArrows2Palette'],['atlassian','addAtlassianPalette']]){family=group;sb[method]();}
   const rows=[];
   for(const [issue,group,title,count,required,wrong]of targets){
    const found=entries.filter(e=>e.family===group&&e.title===title);
    if(found.length!==count)mismatches.push(issue+' '+title+' count '+found.length);
    for(const entry of found){
     const tokens=entry.tags.split(' ');let hit;
     const query=required.join(' ');
     sb.searchEntries(query,1000,0,fns=>{hit=fns.some(fn=>fn.ownedReview===entry);},null,true);
     for(const word of required)if(!tokens.includes(word))mismatches.push(issue+' '+title+' missing '+word);
     for(const word of wrong)if(tokens.includes(word))mismatches.push(issue+' '+title+' retains '+word);
     if(!hit)mismatches.push(issue+' '+title+' missing actual search hit');
     const wrongExact=wrong.filter(word=>sb.taglist[word]?.entries?.some(fn=>fn.ownedReview===entry));
     rows.push({issue,entry,query,hit,wrongExact});
    }
   }
   const controls=[];
   for(const [group,title,token]of [['archimate2','Goal','goal'],['archimate3','Application Component','component'],['archimate3','Application Service','service'],['archimate3','Device','device']]){
    const found=entries.filter(e=>e.family===group&&e.title===title);if(!found.length||found.some(e=>!e.tags.split(' ').includes(token)))mismatches.push('unchanged control '+title);controls.push(...found);
   }
   const badges=entries.filter(e=>e.family==='android'&&/^Contact Badge (Focused|Normal|Pressed)$/.test(e.title));
   if(badges.length!==6)mismatches.push('191 expected6badge templates');
   const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
   const badgeCells=[],referenceCells=[];graph.model.beginUpdate();
   try{
    for(const [column,state]of ['Focused','Normal','Pressed'].entries()){
     for(const [row,size]of [40,60].entries()){
      const entry=badges.find(e=>e.title==='Contact Badge '+state&&e.width===size&&e.height===size);if(!entry)throw Error('missing badge size '+state+'/'+size);
      const cell=graph.insertVertex(graph.getDefaultParent(),null,entry.value,100+column*230,80+row*160,entry.width,entry.height,entry.style);
      graph.insertVertex(graph.getDefaultParent(),null,'Palette '+state+' '+size,70+column*230,155+row*160,150,40,'text;html=0;align=center;');badgeCells.push({entry,state,size,cell});
     }
     const entry=badges.find(e=>e.title==='Contact Badge '+state&&e.width===60),style=entry.style.replace(/shape=mxgraph\.android\.contact_badge_[^;]+/,'shape=mxgraph.android.contact_badge_'+state.toLowerCase());
     referenceCells.push({state,cell:graph.insertVertex(graph.getDefaultParent(),null,'',100+column*230,400,60,60,style)});
     graph.insertVertex(graph.getDefaultParent(),null,'Reference '+state,70+column*230,475,150,40,'text;html=0;align=center;');
    }
   }finally{graph.model.endUpdate();}
   const inspect=cell=>{const state=graph.view.getState(cell),desc=state?.shape?.stencil?.desc;return {shape:state?.style?.shape,stencil:desc?.getAttribute('name'),fills:[...new Set([...state.shape.node.querySelectorAll('[fill]')].map(n=>n.getAttribute('fill').toLowerCase()).filter(v=>v!=='none'))].sort(),visible:state.shape.node.getBoundingClientRect().width>0};};
   const references=referenceCells.map(({state,cell})=>({state,...inspect(cell)}));
   const badgeRender=badgeCells.map(({entry,state,size,cell})=>({entry,state,size,...inspect(cell)}));
   for(const badge of badgeRender){const expected=references.find(e=>e.state===badge.state);if(!badge.visible||badge.stencil!=='Contact Badge '+badge.state||JSON.stringify(badge.fills)!==JSON.stringify(expected.fills))mismatches.push('191 actual '+badge.state+'/'+badge.size+' uses '+badge.stencil);}
   return {rows,controls,badgeRender,references,mismatches,captured:entries.length};
  },{code,names:methods.map(([,n])=>n.split('.').at(-1)),targets});
  assert.equal(new Set(observed.references.map(r=>JSON.stringify(r.fills))).size,3,'actual reference states must have distinct colors');
  await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());await frame.page().screenshot({path:path.join(out,theme+'-badges-'+variant+'.png')});
  const rendered=await frame.evaluate(rows=>{
   const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();const cells=[];
   graph.model.beginUpdate();try{for(const [i,{entry}]of rows.entries()){const x=70+i%5*230,y=80+Math.floor(i/5)*180;cells.push(graph.insertVertex(graph.getDefaultParent(),null,entry.value,x,y,entry.width,entry.height,entry.style));graph.insertVertex(graph.getDefaultParent(),null,entry.family+' '+entry.title,x-20,y+100,190,50,'text;html=0;whiteSpace=wrap;align=center;');}}finally{graph.model.endUpdate();}
   return cells.map(cell=>{const state=graph.view.getState(cell);return {shape:state.style.shape,stencil:state.shape.stencil?.desc?.getAttribute('name'),registered:!!state.shape.stencil||mxCellRenderer.defaultShapes[state.style.shape]===state.shape.constructor,visible:state.shape.node.getBoundingClientRect().width>0};});
  },observed.rows);
  for(const item of rendered)assert.ok(item.registered&&item.visible,variant+' registered/rendered metadata target '+item.shape);
  fs.writeFileSync(path.join(out,theme+'-state-search-'+variant+'.json'),JSON.stringify({...observed,rendered},null,2)+'\n');
  await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());await frame.page().screenshot({path:path.join(out,theme+'-state-search-'+variant+'.png')});
  if(observed.mismatches.length)console.log('State/search diagnostics '+JSON.stringify({variant,theme,mismatches:observed.mismatches}));
  assert.deepEqual(observed.mismatches,[],variant+' state identities and explicit search tags');
  checks.push({case:'191 sixbadge state/size render and distinct references,192–195 actual factory/index/search/render with controls',variant,theme,result:'PASS',badges:6,metadataTargets:observed.rows.length,captured:observed.captured});
 }
 return checks;
};
