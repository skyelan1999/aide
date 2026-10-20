'use strict';
const {chromium}=require('./browser-runtime/node_modules/playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const base=process.env.AIDE_TEST_URL,data=process.env.AIDE_TEST_DATA,out=process.env.AIDE_EVIDENCE_DIR;
if(!base||!data||!out)throw Error('Set isolated fixture paths');
(async()=>{
 fs.mkdirSync(out,{recursive:true});const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:960},locale:'zh-CN'});
  await context.addInitScript(token=>{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,language:'zh-CN',theme:'dark',palette:'blue',motion:'reduced',startupAnimation:'off',sandboxMode:'danger-full-access'}));},fs.readFileSync(path.join(data,'auth/access-token'),'utf8').trim());
  const page=await context.newPage(),errors=[],inputs=[];let commands=0,settings=0;
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{const p=new URL(r.url()).pathname;if(r.method()==='POST'&&p==='/api/command')commands++;if(p==='/api/command/fixture-pty/input')inputs.push(JSON.parse(r.postData()).input);if(r.method()==='PUT'&&p==='/api/settings')settings++;});
  // Display SSH metadata; the backend workspace and all data remain disposable/local.
  await page.route('**/api/workspace-config',async route=>{const response=await route.fetch(),d=await response.json();d.workspace={...d.workspace,mode:'ssh',host:'fixture.invalid',path:'/srv/device',username:'fixture'};await route.fulfill({response,json:d});});
  await page.route('**/api/config',async route=>{const response=await route.fetch(),d=await response.json();d.sandbox.remote=true;await route.fulfill({response,json:d});});
  await page.goto(base,{waitUntil:'networkidle'});await page.locator('#terminal-toggle').click();
  await page.locator('#command-policy').waitFor({state:'visible'});assert.match(await page.locator('#command-policy').innerText(),/SSH.*沙箱/);assert.match(await page.locator('#command-location').innerText(),/远程目录：\/srv\/device/);assert.equal(await page.locator('#command-run').isDisabled(),true);
  await page.locator('#command').fill('pwd');await page.locator('#command').press('Enter');assert.equal(commands,0);assert.equal(settings,0);assert.equal(await page.locator('#command').inputValue(),'pwd');
  for(const width of [1440,390]){await page.setViewportSize({width,height:960});await page.screenshot({path:path.join(out,'ssh-restricted-'+width+'.png')});assert.equal(await page.locator('#command-permissions').isVisible(),true);}
  await page.setViewportSize({width:1440,height:960});await page.locator('#command-permissions').click();
  assert.equal(await page.locator('[data-control="sandboxMode"] [data-value="workspace-write"]').getAttribute('aria-pressed'),'true','stale browser mode must not override server');
  page.once('dialog',d=>d.dismiss());await page.locator('#settings-content').getByText('完全访问',{exact:true}).click();assert.equal(settings,0,'cancel does not change server permissions');
  page.once('dialog',d=>d.accept());await page.locator('#settings-content').getByText('完全访问',{exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#command-policy').hidden);assert.equal(settings,1,'only explicit user selection changes sandbox mode');await page.locator('#settings-sheet-close').click();
  await page.reload({waitUntil:'networkidle'});await page.locator('#terminal-toggle').click();assert.equal(await page.locator('#command-policy').isHidden(),true,'server mode survives page reload');await page.locator('#command').fill('pwd');
  await page.locator('#command').press('Enter');await page.waitForFunction(()=>document.querySelector('#terminal-output').textContent.includes('交互终端已就绪'));assert.equal(commands,1);assert.equal(await page.locator('#command-run').innerText(),'发送');assert.equal(await page.locator('#command-run').isEnabled(),true);
  await page.locator('#command').fill('second command');await page.locator('#command').press('Enter');await page.waitForFunction(()=>document.querySelector('#terminal-output').textContent.includes('fixture input: second command'));
  await page.locator('#command').fill('third command');await page.locator('#command-run').click();await page.locator('#command').fill('ech');await page.locator('#command').press('Tab');await page.locator('#command').press('Control+c');
  await page.waitForFunction(()=>document.querySelector('#terminal-output').textContent.includes('fixture input: ech'));
  assert.deepEqual(inputs.slice(0,4),['second command\r','third command\r','ech\t','\x03']);assert.equal(commands,1,'PTY input must not start new commands');
  await page.screenshot({path:path.join(out,'ssh-interactive.png')});await page.locator('#command-stop').click();await page.waitForFunction(()=>document.querySelector('#command-stop').classList.contains('hidden'));assert.equal(await page.locator('#command-run').innerText(),'运行');
  // An HTTP failure retains the unsent draft and records a persistent error.
  await page.route('**/api/command',r=>r.fulfill({status:403,json:{error:'fixture policy changed'}}));await page.locator('#command').fill('retry draft');await page.locator('#command-run').click();await page.waitForFunction(()=>document.querySelector('#command').value==='retry draft');assert.match(await page.locator('#terminal-output').innerText(),/命令错误.*fixture policy changed/);
  assert.deepEqual(errors,[]);const result={status:'PASS',scope:'Actual Aide UI and HTTP/NDJSON stream with production commandInput forwarding into an owned pipe; SSH metadata is synthetic, no remote SSH process/device was used.',checks:['restricted SSH explains policy before submit','restricted Enter preserves draft and sends no command or settings write','1440/390 readable permissions action','server mode overrides stale localStorage; cancel performs no write','explicit settings choice saves to server and survives reload','pending stream accepts Enter and Send button','Tab/Ctrl+C exact byte forwarding','one command start across interactive input','stop restores controls','403 retains draft and terminal error'],commands,inputs,settings,errors};fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));await context.close();
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
