'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.AIDE_PLAYWRIGHT||path.join(__dirname,'browser-runtime/node_modules/playwright'));
const base=process.env.AIDE_TEST_URL,out=process.env.AUTO_ROUTER_EVIDENCE;
(async()=>{const fixture=JSON.parse(fs.readFileSync(path.join(out,'ready.json'))),browser=await chromium.launch({headless:true}),context=await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'}),errors=[],external=[];
try{
 await context.addInitScript(token=>{if(window.top!==window)return;localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,theme:'light',palette:'blue',language:'zh-CN',startupAnimation:'off',motion:'reduced'}));},fixture.token);
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await context.route('**/*',route=>{const u=route.request().url();if(/^https?:/.test(u)&&new URL(u).origin!==new URL(base).origin){external.push(new URL(u).origin);return route.abort();}return route.continue();});
 const menu=()=>page.locator('#strategy-menu');
 async function open(){if(await menu().isVisible())await page.keyboard.press('Escape');await page.locator('#strategy-button').click();await menu().waitFor({state:'visible'});}
 const models=()=>menu().locator('.strategy-menu-col').nth(1),auto=()=>models().getByRole('menuitemradio',{name:/Auto/});
 async function request(method,url,body){return page.evaluate(async({method,url,body})=>{const r=await fetch('/api'+url,{method,headers:{Authorization:'Bearer '+localStorage.getItem('aide-token'),'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});if(!r.ok)throw Error(await r.text());return r.json();},{method,url,body});}
 await page.goto(base,{waitUntil:'networkidle'});
 await page.locator('[data-session-id="'+fixture.session+'"]').click();await open();assert.equal(await auto().count(),1);assert.match(await models().getByRole('menuitemradio').first().innerText(),/Auto/);
 await auto().click();await page.waitForTimeout(100);let session=await request('GET','/sessions/'+fixture.session);assert.equal(session.modelSelection.modelRouter,'auto');assert.equal(session.modelSelection.model,'deepseek-flash');
 await page.reload({waitUntil:'networkidle'});await open();assert.equal(await auto().getAttribute('aria-checked'),'true');
 await models().getByRole('menuitemradio',{name:/Fixture Claude/}).click();session=await request('GET','/sessions/'+fixture.session);assert.equal(session.modelSelection.model,'claude-sonnet-4-6');assert(!session.modelSelection.modelRouter);
 await open();await auto().click();await open();await menu().getByRole('menuitemradio',{name:/5X/}).click();await open();assert.equal(await auto().getAttribute('aria-checked'),'true');assert.equal(await menu().getByRole('menuitemradio',{name:/5X/}).getAttribute('aria-checked'),'true');
 for(const theme of ['light','dark'])for(const palette of ['blue','green'])for(const width of [1440,390]){await page.setViewportSize({width,height:1000});await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.palette=palette;},{theme,palette});assert.equal(await menu().evaluate(e=>e.scrollWidth>e.clientWidth+1),false);}
 await page.setViewportSize({width:1440,height:1000});await page.screenshot({path:path.join(out,'auto-menu.png')});await page.keyboard.press('Escape');
 await page.locator('#plugins-toggle').click();const card=page.locator('.plugin-card').filter({hasText:'AutoAiModelRouter'});await card.locator('.plugin-toggle').click();await card.locator('.plugin-toggle').filter({hasText:'停用'}).waitFor();await open();assert.equal(await auto().count(),0);await page.keyboard.press('Escape');
 await card.locator('.plugin-toggle').click();await card.locator('.plugin-toggle').filter({hasText:'使用中'}).waitFor();await open();assert.equal(await auto().count(),1);await page.keyboard.press('Escape');
 await request('DELETE','/plugins/auto-ai-model-router');await page.reload({waitUntil:'networkidle'});await open();assert.equal(await auto().count(),0);
 assert.deepEqual(errors,[]);assert.deepEqual(external,[]);fs.writeFileSync(path.join(out,'browser-result.json'),JSON.stringify({status:'PASS',environment:'real isolated Aide testApp + actual public router package files, Chromium',checks:['top conditional Auto','session save and reload','manual exit','5X remains selected','light/dark blue/green desktop/narrow no horizontal overflow','disable/re-enable/uninstall','no external browser requests'],errors,external},null,2)+'\n');console.log('PASS actual host Auto router browser acceptance');
}finally{await context.close();await browser.close();fs.writeFileSync(path.join(out,'done'),'done');}
})().catch(e=>{console.error(e);process.exitCode=1});
