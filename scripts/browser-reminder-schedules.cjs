'use strict';
const {chromium}=require('./browser-runtime/node_modules/playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const base=process.env.AIDE_TEST_URL,data=process.env.AIDE_TEST_DATA,out=process.env.AIDE_EVIDENCE_DIR;
if(!base||!data||!out)throw Error('Set isolated server and evidence paths');
(async()=>{fs.mkdirSync(out,{recursive:true});const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
const context=await browser.newContext({viewport:{width:1440,height:960},locale:'zh-CN',timezoneId:'Asia/Shanghai'}),errors=[];
try{
 await context.addInitScript(token=>{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,theme:'dark',palette:'blue',language:'zh-CN',startupAnimation:'off',motion:'reduced'}));},fs.readFileSync(path.join(data,'auth/access-token'),'utf8').trim());
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(base,{waitUntil:'networkidle'});
 await page.locator('#reminder-center-button').click();await page.locator('#message-tab-reminders').click();await page.locator('#reminder-create-toggle').click();
 assert.equal(await page.locator('#reminder-execution').isVisible(),false);
 await page.locator('#reminder-action').selectOption('execute');assert.equal(await page.locator('#reminder-execution').isVisible(),true);
 assert.equal(await page.locator('#reminder-due').evaluate(e=>e.required),true);assert.equal(await page.locator('#reminder-session-field').isVisible(),false);
 await page.locator('#reminder-target').selectOption('session');await page.locator('#reminder-session-number').fill('987');await page.locator('#reminder-title').fill('设备检查');await page.locator('#reminder-prompt').fill('汇总当前调试结果');
 const due=await page.evaluate(()=>{const d=new Date(Date.now()+3600000);return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16)});await page.locator('#reminder-due').fill(due);
 for(const theme of ['dark','light'])for(const palette of ['blue','comfort'])for(const width of [1440,390]){
  await page.setViewportSize({width,height:960});await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.palette=palette;},{theme,palette});
  await page.locator('#reminder-form').evaluate(e=>e.scrollTop=0);assert.equal(await page.locator('#reminder-save').isEnabled(),true);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.screenshot({path:path.join(out,`${palette}-${theme}-${width}.png`)});
 }
 const responsePromise=page.waitForResponse(r=>r.url().endsWith('/api/reminders')&&r.request().method()==='POST');await page.locator('#reminder-save').click();const response=await responsePromise;assert.equal(response.status(),201);let item=await response.json();
 assert.equal(item.execution.sessionNumber,987);assert.equal(item.execution.sessionId,'schedule-browser');assert.equal(item.execution.state,'pending');assert.equal(item.dueAt,new Date(due+'+08:00').toISOString().replace('.000Z','Z'));
 let card=page.locator('.reminder-item').filter({hasText:'设备检查'});await card.getByRole('button',{name:'编辑',exact:true}).click();assert.equal(await page.locator('#reminder-target').inputValue(),'session');assert.equal(await page.locator('#reminder-session-number').inputValue(),'987');assert.equal(await page.locator('#reminder-due').inputValue(),due);
 await page.locator('#reminder-target').selectOption('xiaomi');await page.locator('#reminder-save').click();await card.getByText('小秘',{exact:true}).waitFor();
 await card.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#reminder-target').selectOption('session');await page.locator('#reminder-session-number').fill('987');await page.locator('#reminder-save').click();await card.getByText('会话 #987',{exact:true}).waitFor();
 await page.request.post(base+'/__schedule_test_tick');
 for(let i=0;i<40;i++){await page.locator('#reminder-refresh').click();if(await card.getByText('执行失败',{exact:true}).count())break;await page.waitForTimeout(100)}
 await card.getByText('执行失败',{exact:true}).waitFor();assert.ok(await card.locator('.reminder-schedule-error').textContent());await page.screenshot({path:path.join(out,'execution-failed-390.png')});
 await card.getByRole('button',{name:'完成',exact:true}).click();await card.waitFor({state:'hidden'});
 await page.locator('#reminder-create-toggle').click();await page.locator('#reminder-title').fill('纯提醒兼容');assert.equal(await page.locator('#reminder-action').inputValue(),'notify');await page.locator('#reminder-save').click();await page.locator('.reminder-item').filter({hasText:'纯提醒兼容'}).waitFor();
 await page.locator('#reminder-create-toggle').click();await page.locator('#reminder-title').fill('取消执行');await page.locator('#reminder-action').selectOption('execute');await page.locator('#reminder-due').fill(due);await page.locator('#reminder-prompt').fill('不应执行');await page.locator('#reminder-save').click();
 const cancelCard=page.locator('.reminder-item').filter({hasText:'取消执行'});await cancelCard.getByRole('button',{name:'取消定时任务',exact:true}).click();await cancelCard.waitFor({state:'hidden'});await page.locator('[data-reminder-status="completed"]').click();await cancelCard.getByText('已取消',{exact:true}).waitFor();
 assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',scope:'Actual isolated Go testApp workbench, actual reminder APIs and scheduler, no real provider or user data',checks:['notify default, execution conditional required fields','existing session number and XiaoMi target round-trip','Shanghai local time round-trip to UTC','1440/390, blue/comfort light/dark, no horizontal overflow','scheduled run errors visible via actual scheduler','pending schedule cancellation recorded, failed result completion and old notify-only creation'],errors},null,2));console.log('PASS actual workbench scheduled reminders');
}finally{await context.close();await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
