'use strict';
const assert=require('node:assert/strict'),path=require('node:path');
module.exports=async function(frame,out,theme,requests){
 const start=requests.length,checks=[];
 await frame.evaluate(()=>{
  window.ownedAIOriginal={actions:Editor.aiActions,models:Editor.aiModels,configs:Editor.aiConfigs,globals:Editor.aiGlobals};
  Editor.aiActions=['create'];Editor.aiModels=[{name:'Owned local mock',config:'owned',model:'owned-model'}];
  Editor.aiGlobals={ownedKey:'owned-test-value'};
  Editor.aiConfigs={owned:{apiKey:'ownedKey',endpoint:location.origin+'/owned-ai-template',requestHeaders:{},
   request:{model:'{model}',messages:[{role:'user',content:'{prompt}'}],optional:null,values:[null,false,0,'']},responsePath:'$.choices[0].message.content'}};
  window.ownedTemplateBefore=JSON.stringify(Editor.aiConfigs.owned.request);
  window.ownedChat=new ChatWindow(ownedUi,10,20,Math.min(innerWidth-20,750),600);ownedChat.window.setVisible(true);
 });
 try{
  const chat=frame.locator('div.mxWindow').filter({has:frame.locator('option[value="model-Owned local mock"]')});
  await chat.waitFor();const input=chat.locator('textarea');
  await input.fill('Owned first prompt');await input.press('Enter');
  try{await chat.getByText('Owned mock answer '+(start+1),{exact:true}).waitFor({timeout:7000});}
  catch(e){await frame.page().screenshot({path:path.join(out,theme+'-ai-failure.png')});throw Error('Actual bundled ChatWindow did not complete null template; mock calls='+ (requests.length-start)+'; UI='+await chat.innerText());}
  assert.equal(requests.length,start+1);assert.equal(requests[start].optional,null);assert.deepEqual(requests[start].values,[null,false,0,'']);
  assert.equal(requests[start].model,'owned-model');assert.equal(requests[start].messages[0].content,'Owned first prompt');
  assert.equal(await frame.evaluate(()=>JSON.stringify(Editor.aiConfigs.owned.request)===ownedTemplateBefore),true);
  checks.push({case:'actual bundled custom model sends null/array/primitive fields and resolves placeholders without template mutation',theme,result:'PASS'});
  // Valid JSON config with an invalid history shape forces a setup exception
  // before sending. It is fixture data, with no real model or credentials.
  await frame.evaluate(()=>Editor.aiConfigs.owned.request={contents:[null]});
  await input.fill('Owned retry prompt');await input.press('Enter');
  const retryTitle=await frame.evaluate(()=>mxResources.get('retry'));
  await chat.locator('img[title='+JSON.stringify(retryTitle)+']').waitFor({timeout:7000});
  assert.match(await chat.innerText(),/Error:.*null/i);assert.equal(requests.length,start+1,'failed setup cannot reach mock transport');
  await frame.page().screenshot({path:path.join(out,theme+'-ai-error-visible.png')});
  await frame.evaluate(()=>Editor.aiConfigs.owned.request=JSON.parse(ownedTemplateBefore));
  await chat.locator('img[title='+JSON.stringify(retryTitle)+']').click();
  await chat.getByText('Owned mock answer '+(start+2),{exact:true}).waitFor({timeout:7000});
  assert.equal(requests.length,start+2);assert.deepEqual(requests[start+1].messages.map(x=>x.role),['user','assistant','user']);
  assert.equal(requests[start+1].messages.at(-1).content,'Owned retry prompt');
  await frame.page().screenshot({path:path.join(out,theme+'-ai-recovered.png')});
  checks.push({case:'setup error visible with retry, no failed transport, corrected template retry preserves conversation',theme,result:'PASS'});
 }finally{
  await frame.evaluate(()=>{ownedChat.destroy();const p=ownedAIOriginal;Editor.aiActions=p.actions;Editor.aiModels=p.models;Editor.aiConfigs=p.configs;Editor.aiGlobals=p.globals;delete window.ownedChat;delete window.ownedAIOriginal;});
 }
 return checks;
};
