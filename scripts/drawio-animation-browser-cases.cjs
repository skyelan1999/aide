'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
module.exports=async function(frame,out,theme){
 const checks=[];
 await frame.evaluate(()=>{
  window.ownedAnimGraph=ownedEditor.graph;window.ownedAnimRoot=ownedAnimGraph.getModel().getRoot();
  window.ownedAnimOriginal=ownedAnimGraph.getAttributeForCell(ownedAnimRoot,'animation');
  window.ownedAnimRaw=JSON.stringify({animation:{loop:false,steps:[null,{fadeTo:null},{flow:null},{wait:1}]}});
  ownedAnimGraph.setAttributeForCell(ownedAnimRoot,'animation',ownedAnimRaw);
  window.ownedAnimCalls=0;window.ownedAnimExecute=ownedAnimGraph.executeCustomActions;
  ownedAnimGraph.executeCustomActions=function(){ownedAnimCalls++;return ownedAnimExecute.apply(this,arguments);};
 });
 try{
  await frame.evaluate(()=>{window.ownedAnim=new AnimationDialog(ownedUi,10,20,Math.min(innerWidth-20,900),650);});
  let dialog=frame.locator('.geAnimationDialog');await dialog.waitFor();
  assert.equal(await dialog.locator('[data-animation-invalid]').count(),3);
  assert.deepEqual(JSON.parse(await dialog.locator('textarea').inputValue()).steps,[null,{fadeTo:null},{flow:null},{wait:1}]);
  const save=await frame.evaluate(()=>mxResources.get('save')),preview=await frame.evaluate(()=>mxResources.get('preview')),del=await frame.evaluate(()=>mxResources.get('delete'));
  assert.equal(await dialog.getByRole('button',{name:save,exact:true}).isDisabled(),true);
  assert.equal(await dialog.getByRole('button',{name:preview,exact:true}).last().isDisabled(),true);
  assert.equal(await frame.evaluate(()=>ownedAnimGraph.getAttributeForCell(ownedAnimRoot,'animation')===ownedAnimRaw&&ownedAnimCalls===0),true);
  await frame.page().screenshot({path:path.join(out,theme+'-animation-invalid.png')});
  for(let i=0;i<3;i++)await dialog.locator('[data-animation-invalid]').first().locator('button[title='+JSON.stringify(del)+']').click();
  assert.equal(await dialog.locator('[data-animation-invalid]').count(),0);
  await dialog.getByRole('button',{name:preview,exact:true}).last().click();await frame.waitForFunction(()=>ownedAnimCalls>0);
  await dialog.getByRole('button',{name:save,exact:true}).click();
  assert.deepEqual(await frame.evaluate(()=>JSON.parse(ownedAnimGraph.getAttributeForCell(ownedAnimRoot,'animation'))),{animation:{steps:[{wait:1}],loop:false}});
  await frame.evaluate(()=>{ownedAnim.window.destroy();delete window.ownedAnim;});
  checks.push({case:'page invalid rows retained, save/preview blocked, delete bad rows, valid preview and save',theme,result:'PASS'});
  await frame.evaluate(()=>{
   window.ownedActionInitial={title:'Owned invalid action',steps:[{fadeTo:null}]};window.ownedActionBefore=JSON.stringify(ownedActionInitial);window.ownedActionSaved=null;
   window.ownedAnim=new AnimationDialog(ownedUi,10,20,Math.min(innerWidth-20,900),650,{kind:'action',showTitle:true,initial:ownedActionInitial,save:data=>{ownedActionSaved=JSON.parse(JSON.stringify(data));}});
  });
  dialog=frame.locator('.geAnimationDialog');await dialog.waitFor();assert.equal(await dialog.locator('[data-animation-invalid]').count(),1);
  const advanced=await frame.evaluate(()=>mxResources.get('editText'));await dialog.getByLabel(advanced,{exact:true}).check();
  await dialog.locator('textarea').fill('{"title":"Corrected owned action","steps":[{"wait":1}]}');
  await dialog.getByRole('button',{name:save,exact:true}).click();
  assert.deepEqual(await frame.evaluate(()=>ownedActionSaved.steps),[{wait:1}]);
  assert.equal(await frame.evaluate(()=>JSON.stringify(ownedActionInitial)===ownedActionBefore),true);
  await frame.evaluate(()=>{ownedAnim.window.destroy();delete window.ownedAnim;});
  checks.push({case:'action mode retains invalid input, raw correction saves without mutating caller',theme,result:'PASS'});
  await frame.evaluate(()=>{
   window.ownedActionSaved=null;
   window.ownedAnim=new AnimationDialog(ownedUi,10,20,Math.min(innerWidth-20,900),650,{kind:'action',initial:{steps:[{wait:1}]},save:data=>{ownedActionSaved=data;}});
  });
  dialog=frame.locator('.geAnimationDialog');await dialog.getByLabel(advanced,{exact:true}).check();
  await dialog.locator('textarea').fill('{"steps":[null,{"flow":null}]}');
  assert.equal(await dialog.getByRole('button',{name:save,exact:true}).isDisabled(),true);
  await frame.evaluate(()=>ownedAnim.window.setVisible(false));assert.equal(await frame.evaluate(()=>ownedActionSaved),null);
  assert.equal(await frame.evaluate(()=>ownedAnim.window.isVisible()),true,'invalid close keeps editor and draft available');
  assert.deepEqual(JSON.parse(await dialog.locator('textarea').inputValue()).steps,[null,{flow:null}]);
  await frame.evaluate(()=>{ownedAnim.window.destroy();delete window.ownedAnim;});
  checks.push({case:'dirty malformed steps cannot autosave through close',theme,result:'PASS'});
  await frame.evaluate(()=>{
   const before=ownedAnimCalls;
   let rejected=false;try{new Editor.AnimationPlayer(ownedAnimGraph,{steps:[null]});}catch(e){rejected=!!e.animationData;}
   if(!rejected||ownedAnimCalls!==before)throw Error('invalid player accepted or ran effects');
   const data={steps:[{wait:1}],loop:false},player=new Editor.AnimationPlayer(ownedAnimGraph,data);data.steps.push(null);
   rejected=false;try{player.play();}catch(e){rejected=!!e.animationData;}
   if(!rejected||player.running||ownedAnimCalls!==before)throw Error('mutated player ran before validation');
  });
  checks.push({case:'player validates before constructor/play effects, including mutation after construction',theme,result:'PASS'});
 }finally{
  await frame.evaluate(()=>{
   if(window.ownedAnim){ownedAnim.window.destroy();delete window.ownedAnim;}
   ownedAnimGraph.executeCustomActions=ownedAnimExecute;
   ownedAnimGraph.setAttributeForCell(ownedAnimRoot,'animation',ownedAnimOriginal);
  });
 }
 return checks;
};
