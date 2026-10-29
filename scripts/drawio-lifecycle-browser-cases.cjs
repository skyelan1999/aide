'use strict';
module.exports=async function(frame,out,theme) {
  const path=require('node:path');
  try {
    await frame.evaluate(()=>{
      const host=document.createElement('div');host.style.cssText='position:fixed;left:40px;top:400px;width:300px;height:180px;z-index:1000;background:white;overflow:hidden';document.body.appendChild(host);
      const graph=new Graph(host),menu=graph.popupMenuHandler,test={graph,host,panCalls:0};window.ownedLifecycleCase=test;
      const pan=graph.panGraph;graph.panGraph=function(){test.panCalls++;return pan.apply(this,arguments);};
      menu.factoryMethod=function(menu){
        test.open=menu.addItem('Owned submenu',null,null);
        test.nested=menu.addItem('Owned nested',null,null,test.open);
        menu.addItem('Owned leaf',null,()=>{},test.nested);
        test.closed=menu.addItem('Owned closed',null,null);menu.addItem('Owned closed leaf',null,()=>{},test.closed);
      };
      menu.popup(70,420,null,new MouseEvent('contextmenu'));
      menu.showSubmenu(menu,test.open);menu.activeRow=test.open;menu.showSubmenu(test.open,test.nested);test.open.activeRow=test.nested;
      test.divs=[menu.div,test.open.div,test.nested.div,test.closed.div];
      test.nodes=test.divs.flatMap(div=>[div,...div.querySelectorAll('*')]);
      if(test.divs.filter(div=>div.isConnected).length!==3)throw new Error('nested menus not open');
      graph.allowAutoPanning=true;graph.timerAutoScroll=true;graph.scrollPointToVisible(500,400);
      test.manager=graph.panningManager;
      if(!test.manager || test.manager.thread==null)throw new Error('lazy auto-pan timer did not start');
      if(!(document.mxListenerList||[]).some(entry=>entry.name==='mouseup'&&entry.f===test.manager.mouseUpListener))throw new Error('document mouseup listener never registered');
    });
    await frame.page().screenshot({path:path.join(out,theme+'-open-submenus.png')});
    const checks=await frame.evaluate(async(theme)=>{
      const test=ownedLifecycleCase;await new Promise(resolve=>setTimeout(resolve,50));
      if(test.panCalls<1)throw new Error('auto-pan never ran');
      test.graph.destroy();const after=test.panCalls;
      if(test.divs.some(div=>div.isConnected)||test.nodes.some(node=>node.mxListenerList&&node.mxListenerList.length))throw new Error('submenu DOM or listeners leaked');
      if(test.manager.thread!==null||test.graph.panningManager!==null)throw new Error('panning manager timer retained');
      if((document.mxListenerList||[]).some(entry=>entry.f===test.manager.mouseUpListener)||
        (test.graph.mouseListeners||[]).includes(test.manager.mouseListener))throw new Error('panning listener retained');
      await new Promise(resolve=>setTimeout(resolve,50));
      if(test.panCalls!==after)throw new Error('pan callbacks survived graph destroy');
      test.host.replaceChildren();
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('width','290');svg.setAttribute('height','100');test.host.appendChild(svg);
      const xml='<shape w="290" h="100" aspect="variable"><foreground><fillcolor color="#0099cc"/><strokecolor color="#003344"/>'+
        '<save/><fillalpha alpha="0.25"/><rect x="10" y="10" w="70" h="70"/><fillstroke/><restore/>'+
        '<save/><strokealpha alpha="0.25"/><rect x="110" y="10" w="70" h="70"/><fillstroke/><restore/>'+
        '<save/><alpha alpha="0.25"/><rect x="210" y="10" w="70" h="70"/><fillstroke/><restore/>'+
        '</foreground></shape>';
      const stencil=new mxStencil(mxUtils.parseXml(xml).documentElement),canvas=new mxSvgCanvas2D(svg);
      stencil.drawShape(canvas,{style:{},outline:false},0,0,290,100);
      const rects=Array.from(svg.querySelectorAll('rect')),alpha=rects.map(rect=>({fill:Number(rect.getAttribute('fill-opacity')||1),stroke:Number(rect.getAttribute('stroke-opacity')||1)}));
      if(JSON.stringify(alpha)!==JSON.stringify([{fill:.25,stroke:1},{fill:1,stroke:.25},{fill:.25,stroke:.25}]))throw new Error('stencil alpha scopes incorrect '+JSON.stringify(alpha));
      return [{case:'actual graph destroy removes open/closed nested popup DOM and all subtree listeners',theme,result:'PASS',menus:test.divs.length},
        {case:'lazy real auto-pan runs, graph destroy stops interval/listeners and no callbacks after50ms',theme,result:'PASS',panCalls:after},
        {case:'real stencil SVG separates fillalpha/strokealpha/global alpha',theme,result:'PASS',alpha}];
    },theme);
    await frame.page().screenshot({path:path.join(out,theme+'-stencil-alpha.png')});
    return checks;
  } finally {
    await frame.evaluate(()=>{const test=window.ownedLifecycleCase;if(test){if(test.manager&&test.manager.thread!=null)clearInterval(test.manager.thread);test.graph.destroy();test.host.remove();delete window.ownedLifecycleCase;}});
  }
};
