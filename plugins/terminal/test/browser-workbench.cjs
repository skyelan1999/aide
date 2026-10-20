'use strict';
// Run against an isolated candidate Aide. Never prints its token or SSH password.
const {chromium}=require('playwright'),{Server}=require('ssh2');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),crypto=require('node:crypto');
const base=process.env.AIDE_TEST_URL||'https://127.0.0.1:8098';
const data=process.env.AIDE_TEST_DATA,output=process.env.AIDE_EVIDENCE_DIR;
if(!data||!output)throw Error('Set AIDE_TEST_DATA and AIDE_EVIDENCE_DIR to isolated acceptance paths');
fs.mkdirSync(output,{recursive:true});
const delay=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,label,timeout=10000){const end=Date.now()+timeout;while(Date.now()<end){if(await fn())return;await delay(40);}throw Error('Timed out: '+label);}
(async()=>{
 const peers=new Set(),password=crypto.randomBytes(18).toString('hex');
 const hostKey=crypto.generateKeyPairSync('rsa',{modulusLength:2048,privateKeyEncoding:{type:'pkcs1',format:'pem'},publicKeyEncoding:{type:'pkcs1',format:'pem'}}).privateKey;
 const ssh=new Server({hostKeys:[hostKey]},client=>{peers.add(client);client.on('close',()=>peers.delete(client));client.on('error',()=>{});
  client.on('authentication',ctx=>ctx.method==='password'&&ctx.username==='device'&&ctx.password===password?ctx.accept():ctx.reject(['password']));
  client.on('ready',()=>client.on('session',accept=>{const s=accept();s.on('pty',accept=>accept());s.on('window-change',accept=>accept?.());s.on('shell',accept=>{const stream=accept();stream.write('DEVICE_READY\r\n$ ');let command='';stream.on('data',chunk=>{for(const c of chunk.toString()){if(c==='\r'||c==='\n'){stream.write('\r\nDEVICE_OK: '+command+'\r\n$ ');command='';}else{command+=c;stream.write(c);}}});});}));});
 await new Promise(resolve=>ssh.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 let page;const errors=[],created=[];let calls=0,renewals=0;
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true,locale:'zh-CN',acceptDownloads:true,viewport:{width:1440,height:960}});
  const token=fs.readFileSync(path.join(data,'auth/access-token'),'utf8').trim();
  await context.addInitScript(({token})=>{try{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,theme:'dark',language:'zh-CN',palette:'blue',startupAnimation:'off',motion:'reduced'}));}catch(_){}},{token});
  page=await context.newPage();page.on('pageerror',error=>errors.push(error.message));
  page.on('request',r=>{if(r.url().includes('/workbench/')&&r.url().endsWith('/call'))calls++;if(r.url().endsWith('/renew'))renewals++;});
  page.on('dialog',dialog=>dialog.accept());
  const frame=()=>page.frameLocator('iframe[title="设备终端工作台"]');
  const text=()=>frame().locator('.terminal-pane:not([hidden]) .xterm-accessibility-tree').innerText();
  async function open(){await page.locator('#plugins-toggle').click();await page.getByRole('button',{name:'打开工作台',exact:true}).click();await until(()=>frame().locator('#new-top').isEnabled(),'bridge readiness');}
  async function send(command){await frame().locator('.terminal-pane:not([hidden]) .xterm-helper-textarea').focus();await page.keyboard.type(command);await page.keyboard.press('Enter');}
  async function create(name,kind='local'){
   await frame().locator('#new-top').click();await frame().locator('#kind').selectOption(kind);await frame().locator('input[name=name]').fill(name);
   if(kind==='ssh'){await frame().locator('input[name=host]').fill('127.0.0.1');await frame().locator('input[name=port]').fill(String(ssh.address().port));await frame().locator('input[name=username]').fill('device');}
   await frame().locator('#create-submit').click();await until(()=>frame().locator('#create-dialog').evaluate(d=>!d.open),'create '+name);
   const row=frame().locator('.session.active').filter({hasText:name});await row.waitFor();const id=await row.getAttribute('data-session-id');created.push(id);return id;
  }
  await page.goto(base,{waitUntil:'domcontentloaded'});await open();
  const a=await create('开发板 A · 本地');await send("export DEVICE=A; printf '\\nDEVICE_%s\\n' \"$DEVICE\"");await until(async()=>/(^|\n)DEVICE_A(\n|$)/m.test(await text()),'local output A');
  const b=await create('构建环境 B');await send("printf '\\nISOLATED_%s\\n' \"${DEVICE:-empty}\"");await until(async()=>/(^|\n)ISOLATED_empty(\n|$)/m.test(await text()),'session isolation');
  // Search and filters affect navigation only; the active terminal stays selected.
  await frame().locator('#session-search').fill('构建环境 B');assert.equal(await frame().locator('.session').count(),1);assert.equal(await frame().locator('.session').getAttribute('data-session-id'),b);
  await frame().locator('#session-search').fill('no-matching-device');assert.equal(await frame().locator('.session').count(),0);await frame().getByText('没有匹配的会话',{exact:true}).waitFor();
  await frame().locator('#session-search').fill('');await frame().locator('[data-filter=ssh]').click();assert.equal(await frame().locator('.session').count(),0);
  await frame().locator('[data-filter=running]').click();assert((await frame().locator('.session').count())>=2);await frame().locator('[data-filter=all]').click();
  await frame().locator('.session[data-session-id="'+a+'"]').click();await send("printf '\\nBACKGROUND_START\\n'; sleep 1; printf '\\nBACKGROUND_COMPLETE\\n'");await until(async()=>/(^|\n)BACKGROUND_START(\n|$)/m.test(await text()),'background command dispatched');
  await page.getByRole('button',{name:'关闭工作台',exact:true}).click();assert.equal(await page.locator('iframe[title="设备终端工作台"]').count(),0);await delay(1400);
  // The plugin pane remains open after closing the overlay.
  await page.getByRole('button',{name:'打开工作台',exact:true}).click();await until(()=>frame().locator('#new-top').isEnabled(),'reopen');await frame().locator('.session[data-session-id="'+a+'"]').click();await until(async()=>/(^|\n)BACKGROUND_COMPLETE(\n|$)/m.test(await text()),'background survived close');
  // Real calls through the actual parent and Go route, crossing the former budget.
  for(let i=0;i<140;i++){const before=await frame().locator('#logging').innerText();await frame().locator('#logging').click();await until(async()=>await frame().locator('#logging').innerText()!==before,'logging toggle');}
  assert(renewals>=1,'bridge capability renewed');assert(calls>140);assert(await frame().locator('#notice').isHidden());
  await frame().locator('#find').click();await frame().locator('#search-input').fill('BACKGROUND_COMPLETE');await frame().locator('#search-next').click();await frame().locator('#search-close').click();
  await frame().locator('#logs').click();await frame().locator('#log-files button').first().click();
  const save=page.getByRole('button',{name:/^保存文件/});await save.waitFor();const downloadPromise=page.waitForEvent('download');await save.click();const download=await downloadPromise;const downloaded=fs.readFileSync(await download.path());assert(downloaded.includes(Buffer.from('BACKGROUND_COMPLETE')));await download.saveAs(path.join(output,'terminal-output.log'));await frame().locator('#close-logs').click();
  await page.screenshot({path:path.join(output,'desktop-dark.png')});
  await page.evaluate(()=>{window.aideUI.set('theme','light');window.aideUI.set('palette','green');});await until(()=>frame().locator('html').getAttribute('data-theme').then(x=>x==='light'),'theme propagation');await page.screenshot({path:path.join(output,'desktop-classic-light.png')});
  const remote=await create('设备 C · SSH','ssh');await until(async()=>/fingerprint|authenticity/.test(await text()),'SSH host verification');await send('yes');await until(async()=>/password:/.test(await text()),'SSH password prompt');await send(password);await until(async()=>/DEVICE_READY/.test(await text()),'SSH shell');await send('device status');await until(async()=>/DEVICE_OK: device status/.test(await text()),'SSH command');assert(!(await text()).includes(password));
  await page.screenshot({path:path.join(output,'ssh-device.png')});
  await page.setViewportSize({width:390,height:844});await until(()=>frame().locator('#sidebar-toggle').isVisible(),'mobile menu');await frame().locator('#sidebar-toggle').click();await frame().locator('.session[data-session-id="'+b+'"]').click();await until(()=>frame().locator('body').evaluate(b=>!b.classList.contains('sidebar-open')),'mobile session switch');await page.screenshot({path:path.join(output,'mobile.png')});
  const noOverflow=await frame().locator('html').evaluate(el=>el.scrollWidth<=el.clientWidth+1);assert(noOverflow,'no horizontal page overflow');
  // A reload also detaches the document and recovers the same live sessions.
  await page.setViewportSize({width:1440,height:960});await page.reload({waitUntil:'domcontentloaded'});await open();await until(async()=>await frame().locator('.session').count()>=3,'reload sessions');await frame().locator('.session[data-session-id="'+a+'"]').click();await until(async()=>/BACKGROUND_COMPLETE/.test(await text()),'reload output replay');
  assert.equal(errors.length,0,errors.join('\n'));
  // Clean up only the sessions this acceptance created; retain their logs for evidence.
  for(const id of created){await frame().locator('.session[data-session-id="'+id+'"]').click();if(await frame().locator('#close-session').isEnabled()){await frame().locator('#close-session').click();await frame().locator('#confirm-accept').click();await until(()=>frame().locator('#close-session').isDisabled(),'session ended');}}
  await frame().locator('.session[data-session-id="'+b+'"]').click();await frame().locator('#delete-session').click();await frame().locator('#confirm-cancel').click();assert.equal(await frame().locator('.session[data-session-id="'+b+'"]').count(),1);
  await frame().locator('#delete-session').click();await frame().locator('#confirm-accept').click();await until(async()=>await frame().locator('.session[data-session-id="'+b+'"]').count()===0,'confirmed deletion');
  await page.getByRole('button',{name:'关闭工作台',exact:true}).click();
  fs.writeFileSync(path.join(output,'browser-result.json'),JSON.stringify({status:'PASS',browser:await browser.version(),calls,renewals,checks:['session search, empty result and running/SSH filters','local PTY input','session isolation','background after close','140 calls with renewal','output search','parent-mediated log download','professional dark','classic light','SSH host confirmation and password','390px mobile switch','reload recovery','explicit session close','cancel and confirm log deletion'],errors},null,2)+'\n');
  console.log('PASS: Chromium workbench acceptance;',calls,'real calls,',renewals,'renewals');
 }catch(error){if(page)await page.screenshot({path:path.join(output,'failure.png')}).catch(()=>{});throw error;}
 finally{await browser.close();for(const client of peers)client.end();await new Promise(resolve=>ssh.close(resolve));}
})().catch(error=>{console.error(error.message);process.exitCode=1;});
