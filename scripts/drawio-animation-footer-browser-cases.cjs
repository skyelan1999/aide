'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base='internal/server/web/vendor/drawio/';
function constructor(file){
 const source=fs.readFileSync(base+'js/'+file,'utf8');
 const a=source.indexOf(file==='diagramly/Dialogs.js'?'var AnimationDialog = function':'AnimationDialog=function');
 const b=source.indexOf(file==='diagramly/Dialogs.js'?'\n};':';AnimationDialog.stepsClipboard=null;',a);
 assert.ok(a>=0&&b>a);return source.slice(a,b+(file==='diagramly/Dialogs.js'?4:0))+';';
}
function labels(language){
 const source=fs.readFileSync(base+'resources/dia'+(language==='en'?'':'_'+language)+'.txt','utf8');
 return Object.fromEntries(source.split(/\r?\n/).filter(s=>/^(preview|reset|cancel|save)=/.test(s)).map(s=>{const i=s.indexOf('=');return [s.slice(0,i),s.slice(i+1)];}));
}
module.exports=async function(frame,out,theme){
 const checks=[];
 await frame.evaluate(()=>{window.ownedFooterOriginal=AnimationDialog;window.ownedFooterGet=mxResources.get;});
 try{
  for(const file of ['app.min.js','integrate.min.js','diagramly/Dialogs.js']){
   await frame.addScriptTag({content:constructor(file)+'\nObject.assign(AnimationDialog,ownedFooterOriginal);'});
   for(const language of ['en','de','zh']){
    const text=labels(language);
    assert.equal(Object.keys(text).length,4);
    for(const [width,height] of [[360,360],[await frame.evaluate(()=>Math.min(innerWidth-20,900)),650]]){
     await frame.evaluate(({text,width,height})=>{
      mxResources.get=function(key){return Object.hasOwn(text,key)?text[key]:ownedFooterGet.apply(this,arguments);};
      window.ownedFooterSaves=[];
      window.ownedFooter=new AnimationDialog(ownedUi,10,20,width,height,{kind:'action',initial:{steps:[{wait:1}]},save:data=>ownedFooterSaves.push(JSON.parse(JSON.stringify(data)))});
     },{text,width,height});
     try{
      const dialog=frame.locator('.geAnimationDialog');await dialog.waitFor();
      const observed=await frame.evaluate(()=>{
       const dialog=ownedFooter.window.div,box=dialog.getBoundingClientRect();
       const buttons=Array.from(document.querySelector('.geAnimationDialog').lastElementChild.querySelectorAll('button,a,img')).map(el=>{
        const r=el.getBoundingClientRect(),hit=document.elementFromPoint(r.right-2,r.top+r.height/2);
        return {text:el.textContent||el.title||el.alt,left:r.left,right:r.right,top:r.top,bottom:r.bottom,width:r.width,height:r.height,hit:hit===el||el.contains(hit)};
       });
       return {dialog:{left:box.left,right:box.right,top:box.top,bottom:box.bottom},buttons};
      });
      const tag=theme+'-'+language+'-'+width+'x'+height+'-'+path.basename(file);
      if(file==='app.min.js')await frame.page().screenshot({path:path.join(out,tag+'-footer.png')});
      fs.writeFileSync(path.join(out,tag+'-footer.json'),JSON.stringify(observed,null,2)+'\n');
      for(const b of observed.buttons){
       assert.ok(b.width>0&&b.height>0&&b.left>=observed.dialog.left&&b.right<=observed.dialog.right&&b.top>=observed.dialog.top&&b.bottom<=observed.dialog.bottom&&b.hit,tag+' clipped/covered control '+JSON.stringify(b));
      }
      const cancel=dialog.getByRole('button',{name:text.cancel,exact:true}),save=dialog.getByRole('button',{name:text.save,exact:true});
      assert.equal((await cancel.boundingBox()).y,(await save.boundingBox()).y,tag+' primary actions stay together');
      await cancel.focus();await cancel.press('Tab');assert.equal(await save.evaluate(el=>el===document.activeElement),true,tag+' Cancel→Save keyboard order');
      await save.press('Enter');assert.deepEqual(await frame.evaluate(()=>ownedFooterSaves.map(data=>data.steps)),[[{wait:1}]]);
      await frame.evaluate(()=>ownedFooter.window.destroy());
      // A real pointer click at the right edge proves Save is not merely
      // represented by a DOM box outside an overflow-hidden ancestor.
      await frame.evaluate(({width,height})=>{window.ownedFooter=new AnimationDialog(ownedUi,10,20,width,height,{kind:'action',initial:{steps:[{wait:1}]},save:data=>ownedFooterSaves.push(JSON.parse(JSON.stringify(data)))});},{width,height});
      const size=await save.boundingBox();await save.click({position:{x:size.width-2,y:size.height/2}});
      assert.equal(await frame.evaluate(()=>ownedFooterSaves.length),2);
      await frame.evaluate(()=>{ownedFooter.window.destroy();window.ownedFooter=new AnimationDialog(ownedUi,10,20,360,360,{kind:'action',initial:{steps:[{wait:1}]},save:()=>{throw Error('Cancel must not save clean data');}});});
      await cancel.click();assert.equal(await frame.evaluate(()=>ownedFooter.window.isVisible()),false);
      checks.push({case:'animation footer visible/hit-tested; Cancel→Save keyboard Enter, Save edge click and clean Cancel',file,theme,language,width,height,result:'PASS'});
     }finally{await frame.evaluate(()=>{if(window.ownedFooter){ownedFooter.window.destroy();delete window.ownedFooter;}});}
    }
   }
  }
 }finally{await frame.evaluate(()=>{window.AnimationDialog=ownedFooterOriginal;mxResources.get=ownedFooterGet;delete window.ownedFooterOriginal;delete window.ownedFooterGet;delete window.ownedFooterSaves;});}
 return checks;
};
