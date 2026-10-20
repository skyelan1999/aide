'use strict';
// Real browser acceptance against an isolated candidate Aide instance.
const {chromium}=require(process.env.AIDE_PLAYWRIGHT_MODULE||require('node:path').join(__dirname,'browser-runtime/node_modules/playwright'));
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const base=process.env.AIDE_TEST_URL,store=process.env.AIDE_TEST_DATA,evidence=process.env.AIDE_EVIDENCE_DIR;
if(!base||!store||!evidence)throw Error('Set AIDE_TEST_URL, AIDE_TEST_DATA and AIDE_EVIDENCE_DIR for an isolated test instance');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,label){for(let i=0;i<250;i++){if(await fn())return;await sleep(40);}throw Error('Timed out: '+label);}
(async()=>{
 fs.mkdirSync(evidence,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 const errors=[];let origin,tab;
 try{
  const context=await browser.newContext({locale:'zh-CN',ignoreHTTPSErrors:true,viewport:{width:1440,height:960},acceptDownloads:true});
  context.on('page',page=>page.on('pageerror',e=>errors.push(e.message)));
  origin=await context.newPage();await origin.goto(base,{waitUntil:'domcontentloaded'});
  const token=fs.readFileSync(path.join(store,'auth/access-token'),'utf8').trim();
  // Set authentication once in the original page: new tabs must use same-origin storage.
  await origin.evaluate(token=>{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,theme:'dark',language:'zh-CN',motion:'reduced',startupAnimation:'off'}));},token);
  await origin.reload({waitUntil:'domcontentloaded'});await origin.locator('#plugins-toggle').click();
  const spawned=context.waitForEvent('page');await origin.locator('#plugin-list').getByRole('button',{name:'新标签页打开',exact:true}).click();tab=await spawned;
  await tab.waitForLoadState('domcontentloaded');assert.equal(new URL(tab.url()).search,'?plugin-workbench=terminal');assert(!tab.url().includes(token));
  assert(await tab.evaluate(()=>window.opener===null));assert.equal(await tab.evaluate(()=>document.referrer),'');
  let frame=tab.frameLocator('iframe[title="设备终端工作台"]');await until(()=>frame.locator('#new-top').isEnabled(),'card new tab auto opens');
  await tab.screenshot({path:path.join(evidence,'card-new-tab.png')});await tab.getByRole('button',{name:'关闭工作台',exact:true}).click();await tab.close();tab=null;
  // The original page keeps its normal overlay behavior and offers the second entry.
  await origin.locator('#plugin-list').getByRole('button',{name:'打开工作台',exact:true}).click();
  const originalFrame=origin.frameLocator('iframe[title="设备终端工作台"]');await until(()=>originalFrame.locator('#new-top').isEnabled(),'normal overlay');
  await originalFrame.locator('#new-top').click();await originalFrame.locator('input[name=name]').fill('新标签页 · 持续调试');await originalFrame.locator('#create-submit').click();await until(()=>originalFrame.locator('#create-dialog').evaluate(d=>!d.open),'local session');
  const id=await originalFrame.locator('.session.active').getAttribute('data-session-id');
  async function send(page,frame,command){await frame.locator('.terminal-pane:not([hidden]) .xterm-helper-textarea').focus();await page.keyboard.type(command);await page.keyboard.press('Enter');}
  const terminalText=frame=>frame.locator('.terminal-pane:not([hidden]) .xterm-accessibility-tree').innerText();
  await send(origin,originalFrame,"printf '\\nTAB_ORIGINAL_READY\\n'");await until(async()=>/(^|\n)TAB_ORIGINAL_READY(\n|$)/m.test(await terminalText(originalFrame)),'original output');
  const popup=context.waitForEvent('page');await origin.locator('.plugin-workbench>header').getByRole('button',{name:'新标签页打开',exact:true}).click();tab=await popup;
  await tab.waitForLoadState('domcontentloaded');frame=tab.frameLocator('iframe[title="设备终端工作台"]');
  await until(()=>frame.locator('.session[data-session-id="'+id+'"]').count().then(n=>n===1),'shared backend session');
  await frame.locator('.session[data-session-id="'+id+'"]').click();
  assert(await tab.evaluate(()=>window.opener===null));assert.equal(await context.pages().length,2);
  assert(await origin.locator('.plugin-workbench').isVisible(),'original overlay remains open');
  await origin.getByRole('button',{name:'关闭工作台',exact:true}).click();await origin.close();origin=null;
  await send(tab,frame,"printf '\\nTAB_SURVIVED_ORIGIN_CLOSE\\n'");await until(async()=>/(^|\n)TAB_SURVIVED_ORIGIN_CLOSE(\n|$)/m.test(await terminalText(frame)),'input after original tab closes');
  await tab.reload({waitUntil:'domcontentloaded'});frame=tab.frameLocator('iframe[title="设备终端工作台"]');await frame.locator('.session[data-session-id="'+id+'"]').click();await until(async()=>/TAB_SURVIVED_ORIGIN_CLOSE/.test(await terminalText(frame)),'deep link reload recovery');
  await tab.screenshot({path:path.join(evidence,'independent-tab.png')});
  await tab.setViewportSize({width:390,height:844});await until(()=>frame.locator('#sidebar-toggle').isVisible(),'narrow screen');
  assert(await tab.locator('.plugin-workbench>header').getByRole('button',{name:'新标签页打开',exact:true}).isVisible());
  await tab.screenshot({path:path.join(evidence,'mobile.png')});assert(await tab.locator('html').evaluate(el=>el.scrollWidth<=el.clientWidth+1));
  await frame.locator('#logs').click();await frame.locator('#log-files button').first().click();const save=tab.getByRole('button',{name:/^保存文件/});await save.waitFor();const pending=tab.waitForEvent('download');await save.click();const download=await pending;assert(fs.readFileSync(await download.path()).includes(Buffer.from('TAB_SURVIVED_ORIGIN_CLOSE')));await frame.locator('#close-logs').click();
  await frame.locator('#close-session').click();await frame.locator('#confirm-accept').click();await until(()=>frame.locator('#close-session').isDisabled(),'explicit close');
  assert.equal(errors.length,0,errors.join('\n'));
  fs.writeFileSync(path.join(evidence,'result.json'),JSON.stringify({status:'PASS',browser:await browser.version(),checks:['card and header buttons','only plugin id in URL','noopener and no referrer','original overlay remains','same-origin login storage','existing session restored','input after original tab closed','new-tab reload','390px layout','log download','explicit session close'],errors},null,2)+'\n');
  console.log('PASS: new-tab workbench, original-tab independence, reload, mobile and log download');
 }catch(error){if(tab&&!tab.isClosed())await tab.screenshot({path:path.join(evidence,'failure.png')}).catch(()=>{});else if(origin&&!origin.isClosed())await origin.screenshot({path:path.join(evidence,'failure.png')}).catch(()=>{});throw error;}
 finally{for(const page of [tab,origin])if(page&&!page.isClosed()){const close=page.getByRole('button',{name:'关闭工作台',exact:true});if(await close.isVisible().catch(()=>false))await close.click().catch(()=>{});await page.close();}await browser.close();}
})().catch(error=>{console.error(error.message);process.exitCode=1;});
