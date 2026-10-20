'use strict';
const {chromium}=require('./browser-runtime/node_modules/playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const base=process.env.AIDE_TEST_URL,data=process.env.AIDE_TEST_DATA,out=process.env.AIDE_EVIDENCE_DIR;
if(!base||!data||!out)throw Error('Set isolated Aide host/data/evidence paths');
(async()=>{
 fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 const errors=[],checks=[],held=new Set();
 try{
  const context=await browser.newContext({viewport:{width:1440,height:960},locale:'zh-CN'});
  await context.addInitScript(token=>{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,theme:'dark',palette:'blue',language:'zh-CN',startupAnimation:'off',motion:'reduced'}));},fs.readFileSync(path.join(data,'auth/access-token'),'utf8').trim());
  const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base,{waitUntil:'networkidle'});
  async function exercise(selector,endpoint,method='GET',{failure=false,submit=false,screenshot}={}){
   const routes=[];const match='**/api/'+endpoint+'*';
   const handler=route=>{if(new URL(route.request().url()).pathname==='/api/'+endpoint&&route.request().method()===method){routes.push(route);held.add(route);}else return route.continue();};
   await page.route(match,handler);
   const button=page.locator(selector);await button.waitFor({state:'visible'});
   const label=await button.textContent();
   // Use a pointer click (including hit testing), then inspect feedback at the first animation frame.
   await button.evaluate(b=>{b.dataset.feedbackMs='pending';b.addEventListener('click',()=>{const start=performance.now();requestAnimationFrame(()=>{b.dataset.feedbackMs=b.hasAttribute('data-action-pending')?String(performance.now()-start):'-1';});},{once:true,capture:true});});
   await button.click();
   await page.waitForFunction(selector=>document.querySelector(selector)?.dataset.feedbackMs!=='pending',selector);
   const ms=Number(await button.getAttribute('data-feedback-ms'));
   assert.ok(ms>=0,selector+' must show pending feedback at the first rendered frame');
   await button.evaluate((b,submit)=>{if(submit){b.form.requestSubmit();b.form.requestSubmit(b);}else{b.click();b.click();}},submit);
   await page.waitForTimeout(150);assert.ok(routes.length>0,'request reached gate: '+endpoint);
   // Sessions fetch active+archived together; all writes and other reads are single requests.
   assert.equal(routes.length,endpoint==='sessions'?2:1,'repeated clicks must not repeat '+endpoint);
   assert.equal(await button.textContent(),label,'waiting preserves accessible label');
   assert.equal(await button.getAttribute('aria-busy'),'true');
   await page.locator('#queue-toggle').evaluate(b=>{b.click();b.click();});
   assert.equal(await page.locator('#queue-toggle').getAttribute('data-action-pending'),null,'independent local navigation stays responsive');
   if(screenshot)await page.screenshot({path:path.join(out,screenshot)});
   for(const route of routes){if(failure)await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'测试：临时不可用'})});else await route.continue();held.delete(route);}
   await page.waitForFunction(selector=>!document.querySelector(selector)?.hasAttribute('data-action-pending'),selector);
   await page.unroute(match,handler);
   checks.push({selector,endpoint,method,result:failure?'failure recovered':'success recovered',feedbackMs:ms,requests:routes.length});
  }
  await exercise('#refresh-sessions','sessions');
  const assistantId=await page.locator('#assistant-entry').getAttribute('data-session-id');
  assert.ok(assistantId,'real assistant session initialized');
  await exercise('#assistant-entry','sessions/'+assistantId+'/unlock-assistant','POST');
  await page.locator('#new-session').click();
  await exercise('#workspace-config-button','workspace-config','GET',{screenshot:'workspace-pending-1440.png'});
  await page.locator('#workspace-sheet-close').click();
  await exercise('#workspace-config-button','workspace-config','GET',{failure:true});
  assert.equal(await page.locator('#workspace-sheet').evaluate(n=>n.classList.contains('open')),false);
  if(!await page.locator('#refresh-files').isVisible())await page.locator('#files-toggle').click();
  await exercise('#refresh-files','files');
  await page.locator('#plugins-toggle').click();
  await exercise('.plugin-card .plugin-toggle','plugins/feedback-fixture','PUT');
  await page.locator('.plugin-card .plugin-settings-button').click();
  await exercise('#plugin-settings-form .primary','plugins/feedback-fixture/settings','PUT',{submit:true});
  await page.locator('#reminder-center-button').click();
  await page.locator('#message-tab-reminders').click();
  await page.locator('#reminder-create-toggle').click();
  await page.locator('#reminder-title').fill('按钮反馈验收（隔离数据）');
  await exercise('#reminder-save','reminders','POST',{submit:true});
  await page.locator('#reminder-center-close').click();
  await page.setViewportSize({width:390,height:844});
  // Reuse the same page; narrow-screen refresh remains usable and does not move the label.
  if(!await page.locator('#refresh-files').isVisible())await page.locator('#files-toggle').click();
  await exercise('#refresh-files','files','GET',{failure:true,screenshot:'files-pending-390.png'});
  await page.locator('#file-panel-close').click();
  await page.locator('#sidebar-toggle').click();
  const schema=[];const holdSchema=route=>{schema.push(route);held.add(route);};
  await page.route('**/settings-schema.json',holdSchema);
  await page.locator('#brand-button').click();
  await page.locator('#settings-sheet-close').click();
  await page.locator('#brand-button').click();
  assert.equal(await page.locator('#settings-sheet').evaluate(n=>n.classList.contains('open')),true,'settings can reopen before schema arrives');
  await page.waitForTimeout(150);assert.equal(schema.length,1,'close/reopen shares one schema request');
  await schema[0].continue();held.delete(schema[0]);await page.unroute('**/settings-schema.json',holdSchema);
  await page.waitForFunction(()=>!document.querySelector('#settings-content').hasAttribute('aria-busy'));
  await page.waitForFunction(()=>document.querySelector('#settings-sheet').classList.contains('open'));
  checks.push({selector:'#brand-button',result:'narrow sidebar settings opens, closes and reopens while schema is pending; one shared request'});
  await page.locator('#settings-sheet-close').click();
  await page.setViewportSize({width:1440,height:960});
  for(const [palette,theme] of [['blue','dark'],['blue','light'],['comfort','dark'],['comfort','light']]){
   await page.evaluate(([palette,theme])=>window.aideUI.setAppearance(palette,theme),[palette,theme]);
   await exercise('#refresh-sessions','sessions','GET',{screenshot:'pending-'+palette+'-'+theme+'.png'});
   checks[checks.length-1].appearance={palette,theme};
  }
  assert.deepEqual(errors,[]);
  const result={status:'PASS',timing:'feedbackMs measures click to first animation frame with pending indicator, not network completion or a hardware-independent latency bound',scope:'Actual embedded Aide UI in disposable Go testApp; API requests held for 150ms and then continued against real test backend or failed deliberately. No paid models or user data.',checks,errors};
  fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));await context.close();
 }finally{for(const route of held)await route.abort().catch(()=>{});await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
