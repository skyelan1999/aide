'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
const review=require('./drawio-fifty-palette-review.cjs');
module.exports=async function(frame,out,theme){
 const checks=[];
 for(const variant of ['source','app.min.js','integrate.min.js']){
  const code=review.methods.map(([file,name])=>assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+(variant==='source'?file:variant),'utf8'),name));
  const observed=await frame.evaluate(async({code,families,targets})=>{
   function Captured(){}Captured.prototype=Object.create(Sidebar.prototype);
   const sb=new Captured(),entries=[],mismatches=[];sb.taglist=Object.create(null);sb.searchIndexData=null;sb.setCurrentSearchEntryLibrary=()=>{};sb.addPaletteFunctions=()=>{};sb.addPalette=()=>{};sb.graph=ownedUi.sidebar.graph;
   Function('Sidebar','sb',code.join('\n'))(Captured,sb);let family;
   for(const [method,kind,index]of [['createVertexTemplateEntry','vertex',7],['createEdgeTemplateEntry','edge',6]]){
    const original=Sidebar.prototype[method];sb[method]=function(style,width,height,value,title){const fn=original.apply(this,arguments),entry={family,kind,style,width,height,value,title,tags:arguments[index]||''};entry.fn=fn;fn.ownedReview=entry;entries.push(entry);return fn;};
   }
   for(const [name,method]of families){family=name;sb[method]();}
   const rows=[];const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
   for(const spec of targets){
    const matches=entries.filter(e=>e.family===spec.family&&(!spec.marker||e.style.includes(spec.marker))&&(!spec.selectTitle||e.title===spec.selectTitle));
    if(matches.length!==(spec.count||1))mismatches.push(spec.issue+' target count '+spec.title+' '+matches.length);
    for(const e of matches){
     const prefix=spec.issue+' '+spec.title;if(e.title!==spec.title)mismatches.push(prefix+' title '+e.title);
     const tokens=e.tags.toLowerCase().split(/\s+/);for(const t of spec.required||[])if(!tokens.includes(t))mismatches.push(prefix+' missing '+t);for(const t of spec.forbidden||[])if(tokens.includes(t))mismatches.push(prefix+' wrong '+t);
     for(const t of spec.styleRequired||[])if(!e.style.includes(t))mismatches.push(prefix+' missing style '+t);for(const t of spec.styleForbidden||[])if(e.style.includes(t))mismatches.push(prefix+' bad style '+t);
     const query=spec.query||spec.title;let hit=false;sb.searchEntries(query,1000,0,fns=>{hit=fns.includes(e.fn);},null,true);if(!hit)mismatches.push(prefix+' query misses '+query);
     const i=rows.length,cell=graph.insertVertex(graph.getDefaultParent(),null,e.value,50+i%5*230,60+Math.floor(i/5)*240,e.width,e.height,e.style);
     const state=graph.view.getState(cell);if(!state?.shape?.node?.isConnected)mismatches.push(prefix+' not rendered');
     const names={};for(const k of ['shape','resIcon','prIcon']){const value=state?.style?.[k];if(value?.startsWith('mxgraph.')){let key=value;if(k==='prIcon')key=(state.style.shape||'').replace(/\.[^.]+$/,'')+'.'+value;const stencil=mxStencilRegistry.getStencil(key);names[k]={key,name:stencil?.desc?.getAttribute('name')};}}
     if(spec.issue===206&&names.shape?.name?.toLowerCase().indexOf('motor starter')!==0)mismatches.push(prefix+' independent stencil identity');
     if(spec.issue===210&&spec.styleRequired&&!names.resIcon?.name?.includes('intrusion prevention'))mismatches.push(prefix+' actual prevention resource missing');
     graph.insertVertex(graph.getDefaultParent(),null,e.title,30+i%5*230,180+Math.floor(i/5)*240,200,50,'text;html=0;whiteSpace=wrap;align=center;');
     rows.push({issue:spec.issue,entry:{...e,fn:undefined},query,hit,names,cellId:cell.id});
    }
   }
   const camera=rows.find(r=>r.issue===209);let fillControls=[];
   if(camera){const original=StyleFormatPanel.prototype.createCellColorOption;StyleFormatPanel.prototype.createCellColorOption=function(label,key){fillControls.push(key);return original.apply(this,arguments);};try{graph.setSelectionCell(graph.model.getCell(camera.cellId));ownedUi.format.immediateRefresh();}finally{StyleFormatPanel.prototype.createCellColorOption=original;}if(!fillControls.includes('fillColor2'))mismatches.push('209 actual Format panel secondary color missing');}
   const controls=entries.filter(e=>e.family==='Cabinet'&&e.style.includes('motor_cb_')||e.family==='Cisco19'&&e.style.includes('prIcon=monitoring;')||e.family==='Electrical'&&e.style.includes('.equipotential;')||e.family==='Floorplan'&&e.style.includes('doorLeft;')).map(e=>({...e,fn:undefined}));
   ownedUi.setPageVisible(false);ownedUi.actions.get('fitWindow').funct();
   return {mismatches,rows,controls,fillControls,captured:entries.length};
  },{code,families:review.families,targets:review.targets});
  fs.writeFileSync(path.join(out,theme+'-fifty-palette-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');await frame.page().screenshot({path:path.join(out,theme+'-fifty-format-'+variant+'.png')});
  await frame.evaluate(()=>{ownedEditor.graph.clearSelection();ownedUi.actions.get('fitWindow').funct();});await frame.page().screenshot({path:path.join(out,theme+'-fifty-palette-'+variant+'.png')});
  if(observed.mismatches.length)console.log(JSON.stringify({theme,variant,mismatches:observed.mismatches}));
  assert.deepEqual(observed.mismatches,[],variant+' palette identities, search, actual resource rendering and Format controls');
  checks.push({case:'206–215 actual factories/search/resource/render and secondary Fill UI',variant,theme,result:'PASS',targets:observed.rows.length});
 }
 return checks;
};
