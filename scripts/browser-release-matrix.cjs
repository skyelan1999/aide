'use strict';
// Real product DOM, HTTP and persistence. The provider is deterministic and
// isolated by the Go fixture; dispatching browser drop/paste is synthetic input.
const engines=require('./browser-runtime/node_modules/playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const base=process.env.AIDE_TEST_URL,token=process.env.AIDE_TEST_TOKEN,out=process.env.AIDE_EVIDENCE_DIR;
if(!base||!token||!out)throw Error('Use the isolated Go Release browser fixture');
const report={status:'fail',commit:process.env.AIDE_TEST_COMMIT||require('node:child_process').execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim(),cases:[],limits:['Synthetic provider, no paid inference','Linux WebKit does not prove native Safari or OS permission grants']};
fs.mkdirSync(out,{recursive:true});
(async()=>{
 let index=0;
 for(const name of (process.env.AIDE_BROWSER_DEBUG_ENGINE?[process.env.AIDE_BROWSER_DEBUG_ENGINE]:['chromium','firefox','webkit'])){
  const browser=await engines[name].launch({headless:true});
  try{for(const language of ['zh-CN','en'])for(const palette of ['blue','comfort'])for(const theme of ['light','dark'])for(const width of [1440,390]){
   const id=index++,label=[name,language,palette,theme,width].join('-');
   const row={id:label,status:'fail',checks:[]};report.cases.push(row);
   let page;let phase="initial";const readRequests=new Set();let oldDocumentReads=new Set();
   const context=await browser.newContext({viewport:{width,height:960},locale:language==='en'?'en-US':'zh-CN',reducedMotion:'reduce'});
   try{
    await context.addInitScript(({token,language,palette,theme})=>{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,language,palette,theme,motion:'reduced',startupAnimation:'off'}));},{token,language,palette,theme});
    page=await context.newPage();const errors=[];page.setDefaultTimeout(15000);page.on('pageerror',error=>errors.push({message:error.message,phase}));row.failedRequests=[];page.on('request',request=>{const url=new URL(request.url());if(request.method()==='GET'&&url.origin===new URL(base).origin)readRequests.add(url.pathname+url.search);});page.on('requestfailed',request=>row.failedRequests.push({path:new URL(request.url()).pathname,error:request.failure()?.errorText,phase}));
    await page.goto(base,{waitUntil:'networkidle'});
    // Select a real persisted pending shell approval, inspect original command,
    // and reject through the product endpoint (not a route stub).
    if(width===390)await page.locator('#sidebar-toggle').click();
    await page.locator('[data-session-id="release-'+id+'"]').click();
    if(width===390&&await page.locator('#sidebar-scrim').isVisible())await page.locator('#sidebar-scrim').click({position:{x:width-8,y:80}});
    const card=page.locator('.shell-approval-card');await card.waitFor();
    await card.locator('details.task-command-detail summary').click();
    assert.match(await card.locator('.approval-command').innerText(),/printf release-confirmation/);
    assert.equal(await card.locator('.approval-intent').count(),1);
    const answer=page.waitForResponse(r=>r.url().includes('/approval-'+id+'/answer')&&r.request().method()==='POST');
    await card.locator('.approval-deny').click();assert.equal((await answer).status(),200);
    row.checks.push('manual intent and original command; exact-round rejection reaches real backend');
    // A new conversation and a real multipart/chunk upload from a drop event.
    if(width===390)await page.locator('#sidebar-toggle').click();
    await page.locator('#new-session').click();
    if(width===390&&await page.locator('#sidebar-scrim').isVisible())await page.locator('#sidebar-scrim').click({position:{x:width-8,y:80}});
    const drop=await page.evaluateHandle(()=>{const d=new DataTransfer();d.items.add(new File(['release attachment fixture'],'release-drop.txt',{type:'text/plain'}));return d;});
    await page.locator('#task-form').dispatchEvent('drop',{dataTransfer:drop});await drop.dispose();
    await page.waitForFunction(()=>document.querySelector('#attachment-chips').textContent.includes('release-drop.txt'));
    await page.evaluate(()=>{const d=new DataTransfer();const bytes=Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jF9sAAAAASUVORK5CYII='),c=>c.charCodeAt(0));d.items.add(new File([bytes],'release-paste.png',{type:'image/png'}));const event=new Event('paste',{bubbles:true,cancelable:true});Object.defineProperty(event,'clipboardData',{value:d});document.querySelector('#prompt').dispatchEvent(event);});
    await page.waitForFunction(()=>document.querySelector('#attachment-chips').textContent.includes('.png'));
    row.checks.push('file drop and image paste upload and become actual attachments without model-name gate');
    await page.locator('#prompt').fill('Release browser deterministic request');
    const submitted=page.waitForResponse(r=>r.url().endsWith('/runs')&&r.request().method()==='POST');
    await page.locator('#send').click();const response=await submitted;assert.equal(response.status(),202);
    const request=JSON.parse(response.request().postData());assert.equal(request.attachments.length,2);
    await page.locator('#timeline').getByText('RELEASE_BROWSER_REPLY:',{exact:false}).first().waitFor({timeout:45000});
    const run=await response.json();const sessionID=run.sessionId||new URL(response.url()).pathname.split('/')[3];
    oldDocumentReads=new Set(readRequests);readRequests.clear();phase='reload';await page.reload({waitUntil:'networkidle'});phase='active';
    if(width===390)await page.locator('#sidebar-toggle').click();
    await page.locator('[data-session-id="'+sessionID+'"]').click();
    if(width===390&&await page.locator('#sidebar-scrim').isVisible())await page.locator('#sidebar-scrim').click({position:{x:width-8,y:80}});
    await page.locator('#timeline').getByText('RELEASE_BROWSER_REPLY:',{exact:false}).first().waitFor();
    row.checks.push('chat sends both attachments, renders streamed Markdown and survives page reload');
    const unauth=await context.request.get(base+'/api/sessions');assert.equal(unauth.status(),401);
    const boundary=await context.request.get(base+'/api/file?root=workspace&path=../outside',{headers:{Authorization:'Bearer '+token}});assert.ok([400,403].includes(boundary.status()));
    row.checks.push('unauthenticated session access and workspace traversal rejected');
    const prompt=await page.locator('#prompt').boundingBox();assert.ok(prompt&&prompt.width>100&&prompt.x>=0&&prompt.x+prompt.width<=width+1);
    // WebKit reports canceled same-origin read fetches as page errors while
    // destroying the old document on reload. Keep these explicit in evidence;
    // never suppress active-document, new-document or script exceptions. Only observed same-origin GETs
    // from the destroyed document can qualify, not arbitrary URL-like errors.
    const prefix='/'+new URL(base).host,suffix=' due to access control checks.';
    const navigationErrors=errors.filter(error=>name==='webkit'&&error.phase==='reload'&&error.message.startsWith(prefix+'/api/')&&error.message.endsWith(suffix)&&oldDocumentReads.has(error.message.slice(prefix.length,-suffix.length)));
    row.navigationCancellations=navigationErrors.map(error=>({phase:error.phase,path:new URL(error.message.slice(prefix.length,-suffix.length),base).pathname}));
    assert.deepEqual(errors.filter(error=>!navigationErrors.includes(error)),[]);row.checks.push('composer stays in viewport and no active-document script errors; known WebKit reload cancellations separately recorded');
    await page.screenshot({path:path.join(out,label+'.png')});row.status='pass';console.log('PASS '+label);
   }catch(error){row.error=String(error);if(page){await page.screenshot({path:path.join(out,label+'-failed.png')}).catch(()=>{});row.visibleText=(await page.locator('body').innerText().catch(()=>'' )).slice(-3500);}throw error;}finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(report.cases.length,48);assert.ok(report.cases.every(c=>c.status==='pass'));report.status='pass';
 console.log('PASS: 48 real browser/locale/palette/theme/viewport scenarios');
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(()=>fs.writeFileSync(path.join(out,'report.json'),JSON.stringify(report,null,2)+'\n'));
