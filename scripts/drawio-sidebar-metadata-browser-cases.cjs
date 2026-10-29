'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs'),{methods,initializers}=require('./drawio-sidebar-metadata-review.cjs');
module.exports=async function(frame,out,theme){
  const checks=[];
  for(const variant of ['source','app.min.js','integrate.min.js']){
    const code=[...methods,...initializers].map(([file,name])=>assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+(variant==='source'?file:variant),'utf8'),name));
    const observed=await frame.evaluate(async({code,selected,initializers})=>{
      function CapturedSidebar(){}CapturedSidebar.prototype=Object.create(Sidebar.prototype);
      Function('Sidebar',code.join('\n'))(CapturedSidebar);
      const sb=new CapturedSidebar(),entries=[],mismatches=[];sb.taglist=Object.create(null);
      for(const key of Object.getOwnPropertyNames(Sidebar.prototype))if(/^(addAWS4b|addAlibabaCloud|addAlliedTelesis)/.test(key)&&key.endsWith('Palette')&&!selected.includes(key)&&!initializers.includes(key))sb[key]=()=>{};
      sb.setCurrentSearchEntryLibrary=()=>{};sb.addPaletteFunctions=()=>{};sb.addPalette=()=>{};
      for(const [method,kind,tagIndex]of [['createVertexTemplateEntry','vertex',7],['createEdgeTemplateEntry','edge',6]]){
        const original=Sidebar.prototype[method];
        sb[method]=function(style,width,height,value,title){
          const fn=original.apply(this,arguments),entry={kind,style,width,height,value,title,tags:arguments[tagIndex]};entries.push(entry);fn.ownedReview=entry;return fn;
        };
      }
      sb.addAWS4bPalette();sb.addActiveDirectoryPalette();sb.addAlibabaCloudPalette();sb.addAlliedTelesisPalette();
      const targets=[];
      const search=(query,entry)=>{let found;sb.searchEntries(query,1000,0,fns=>{found=fns.some(fn=>fn.ownedReview===entry);},null,true);return found;};
      for(const [marker,name]of [['block','Default'],['openThin','Open (thin)'],['open','Open']]){
        const entry=entries.find(e=>e.kind==='edge'&&e.style.includes('endArrow='+marker+';')&&e.style.includes('startArrow=none;'));
        const title=name==='Open (thin)'?'Open (thin, right)':name==='Default'?'Default (right)':'Open (right)';
        if(entry.title!==title||!entry.tags.split(' ').includes('right')||entry.tags.split(' ').includes('left'))mismatches.push('186 right title/tags '+marker);
        if(!search('right',entry))mismatches.push('186 missing right search '+marker);targets.push(entry);
      }
      const patch=entries.filter(e=>e.style.includes('resIcon=mxgraph.aws4.patch_manager;'));
      if(patch.length!==1)mismatches.push('187 Patch Manager count '+patch.length);targets.push(patch[0]);
      for(const [asset,query]of [['cluster_server.svg','cluster server'],['interface.svg','interface']]){
        const entry=entries.find(e=>e.style.includes('/active_directory/'+asset+';'));
        if(entry.tags.split(' ').includes('active')||query.split(' ').some(word=>!entry.tags.split(' ').includes(word)))mismatches.push('188 wrong specific tags '+asset);
        if(!search(query,entry))mismatches.push('188 missing search '+asset);targets.push(entry);
      }
      for(const [needle,title,query,typo,issue]of [
        ['shape=mxgraph.alibaba_cloud.acms_application_configuration_manangement;','ACMS Application Configuration Management','acms application configuration management','manangement',189],
        ['/Vdeo_Conference_Terminal.svg;','Video Conference Terminal','video conference terminal','vdeo',190]]){
        const entry=entries.find(e=>e.style.includes(needle));
        if(entry.title!==title||entry.tags.split(' ').includes(typo)||query.split(' ').some(word=>!entry.tags.split(' ').includes(word)))mismatches.push(issue+' spelling/title');
        if(!search(query,entry))mismatches.push(issue+' missing corrected search');targets.push(entry);
      }
      const left=entries.filter(e=>e.kind==='edge'&&e.style.includes('endArrow=none;')&&e.style.match(/startArrow=(block|openThin|open);/));
      if(left.length!==3||left.some(e=>!e.title.includes('left')||!e.tags.split(' ').includes('left')))mismatches.push('186 left controls changed');
      const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
      const cells=[];graph.model.beginUpdate();
      try{
        for(const [i,entry]of targets.entries()){
          const x=80+i%4*210,y=100+Math.floor(i/4)*220;let cell;
          if(entry.kind==='edge'){
            cell=graph.insertEdge(graph.getDefaultParent(),null,'',null,null,entry.style);
            const geo=cell.geometry.clone();geo.setTerminalPoint(new mxPoint(x,y),true);geo.setTerminalPoint(new mxPoint(x+entry.width,y),false);graph.model.setGeometry(cell,geo);
          }else cell=graph.insertVertex(graph.getDefaultParent(),null,entry.value,x,y,entry.width,entry.height,entry.style);
          graph.insertVertex(graph.getDefaultParent(),null,(entry.title||entry.value).replace(/\n/g,' '),x-20,y+110,160,60,'text;html=0;whiteSpace=wrap;align=center;verticalAlign=top;');cells.push(cell);
        }
      }finally{graph.model.endUpdate();}
      const rendered=[];
      for(const cell of cells){
        const state=graph.view.getState(cell);if(!state?.shape?.node||!state.shape.node.getBoundingClientRect().width)mismatches.push('missing rendered cell');
        if(state.style.image)await new Promise((resolve,reject)=>{const image=new Image();image.onload=resolve;image.onerror=()=>reject(Error('image asset failed '+state.style.image));image.src=state.style.image;});
        rendered.push({style:state.style.shape,image:state.style.image,startArrow:state.style.startArrow,endArrow:state.style.endArrow,stencil:state.style.resIcon?mxStencilRegistry.getStencil(state.style.resIcon)?.desc?.getAttribute('name'):state.shape.stencil?.desc?.getAttribute('name')});
      }
      return {mismatches,targets,rendered,patchCount:patch.length,totalCaptured:entries.length,leftControls:left};
    },{code,selected:methods.map(([,n])=>n.split('.').at(-1)),initializers:initializers.map(([,n])=>n.split('.').at(-1))});
    fs.writeFileSync(path.join(out,theme+'-sidebar-metadata-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');
    await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());await frame.page().screenshot({path:path.join(out,theme+'-sidebar-metadata-'+variant+'.png')});
    if(observed.mismatches.length)console.log('Sidebar metadata diagnostics '+JSON.stringify({variant,theme,mismatches:observed.mismatches}));
    assert.deepEqual(observed.mismatches,[],variant+' sidebar metadata/search/render');
    checks.push({case:'186–190 actual initializer/factory/index/search/8target render, unchanged left controls',variant,theme,result:'PASS',patchCount:observed.patchCount});
  }
  return checks;
};
