'use strict';
// Visual and responsive acceptance of the actual sandboxed plugin in an isolated host.
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const base=process.env.AIDE_TEST_URL,data=process.env.AIDE_TEST_DATA,out=process.env.AIDE_EVIDENCE_DIR;
if(!base||!data||!out)throw Error('Set isolated AIDE_TEST_URL, AIDE_TEST_DATA and AIDE_EVIDENCE_DIR');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,label){for(let i=0;i<250;i++){if(await fn())return;await sleep(40);}throw Error('Timed out: '+label);}
(async()=>{
 fs.mkdirSync(out,{recursive:true});const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
 const errors=[],checks=[];let frame,id,page;
 try{
  const ctx=await browser.newContext({ignoreHTTPSErrors:true,locale:'zh-CN',viewport:{width:1440,height:960}});page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base,{waitUntil:'domcontentloaded'});await page.evaluate(token=>{localStorage.setItem('aide-token',token);localStorage.setItem('aide.ui',JSON.stringify({version:1,theme:'dark',palette:'blue',language:'zh-CN',startupAnimation:'off',motion:'reduced'}));},fs.readFileSync(path.join(data,'auth/access-token'),'utf8').trim());
  await page.reload({waitUntil:'domcontentloaded'});await page.locator('#plugins-toggle').click();await page.getByRole('button',{name:'打开工作台',exact:true}).last().click();frame=page.frameLocator('iframe[title="设备终端工作台"]');await until(()=>frame.locator('#new-top').isEnabled(),'ready');
  assert.equal(await page.getByRole('button',{name:'在新标签页打开',exact:true}).count(),0,'plugin new-tab buttons removed');
  await frame.locator('#new-top').click();await frame.locator('input[name=name]').fill('本地开发环境');await frame.locator('#create-submit').click();await until(()=>frame.locator('#create-dialog').evaluate(d=>!d.open),'create');id=await frame.locator('.session.active').getAttribute('data-session-id');
  await frame.locator('.terminal-pane:not([hidden]) .xterm-helper-textarea').focus();await page.keyboard.type("printf '\\n'; uname -srm; printf '\\n'; python3 --version; printf '\\n'; ls -lh plugins/terminal/");await page.keyboard.press('Enter');
  await until(async()=>/manifest.json/.test(await frame.locator('.terminal-pane:not([hidden]) .xterm-accessibility-tree').innerText()),'real shell output');
  assert(await frame.locator('.inspector').isVisible());assert.equal(await frame.locator('#detail-kind').innerText(),'本地 Shell / PTY');assert.equal(await frame.locator('#detail-state').innerText(),'运行中');
  for(const [theme,palette,name] of [['dark','blue','professional-dark'],['light','blue','professional-light'],['dark','green','classic-dark'],['light','green','classic-light']]){
   await page.evaluate(({theme,palette})=>{window.aideUI.set('theme',theme);window.aideUI.set('palette',palette);},{theme,palette});await until(()=>frame.locator('html').getAttribute('data-theme').then(v=>v===theme),'theme');await sleep(350);
   await page.screenshot({path:path.join(out,name+'.png')});
   const colors=await frame.locator('.session.active').evaluate(el=>{const s=getComputedStyle(el);return{background:s.backgroundColor,text:s.color};});
   // WCAG contrast calculation for the selected session label, including translucent mix.
   const rgb=c=>c.startsWith('color(srgb')?c.match(/[\d.]+/g).map(Number).slice(0,3):c.match(/[\d.]+/g).map(Number).slice(0,3).map(v=>v/255);
   const lum=c=>rgb(c).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4).reduce((sum,v,i)=>sum+v*[.2126,.7152,.0722][i],0);
   const a=lum(colors.text),b=lum(colors.background),contrast=(Math.max(a,b)+.05)/(Math.min(a,b)+.05);assert(contrast>=4.5,name+' selected label contrast '+contrast);checks.push({theme:name,selectedLabelContrast:+contrast.toFixed(2)});
  }
  await page.evaluate(()=>{window.aideUI.set('theme','dark');window.aideUI.set('palette','blue');});
  for(const width of [1024,768,390]){
   await page.setViewportSize({width,height:844});await sleep(200);await until(()=>frame.locator('html').evaluate(el=>el.scrollWidth<=el.clientWidth+1),'settled layout at '+width);const overflow=await frame.locator('html').evaluate(el=>({width:el.clientWidth,scroll:el.scrollWidth,boxes:[...document.querySelectorAll('body,main,.content,#tabs,.toolbar,.tools,#terminal-area,#terminal')].map(e=>({id:e.id,cls:e.className,w:e.clientWidth,scroll:e.scrollWidth,overflow:getComputedStyle(e).overflow,x:e.getBoundingClientRect().x,right:e.getBoundingClientRect().right})),items:[...document.querySelectorAll('body *')].filter(e=>!e.closest('dialog')&&e.getBoundingClientRect().right>el.clientWidth+1).map(e=>({tag:e.tagName,id:e.id,cls:e.className,right:e.getBoundingClientRect().right})).slice(0,12)}));if(overflow.scroll>overflow.width+1){console.log(JSON.stringify(overflow));await page.locator('iframe[title="设备终端工作台"]').screenshot({path:path.join(out,'overflow-'+width+'.png')});}assert(overflow.scroll<=overflow.width+1,'overflow at '+width);
   for(const name of ['find','logging','logs','close-session'])assert(await frame.locator('#'+name).isVisible(),name+' visible at '+width);
   if(width===390){await frame.locator('#sidebar-toggle').click();assert.equal(await frame.locator('#sidebar-toggle').getAttribute('aria-expanded'),'true');await frame.locator('#session-search').fill('本地开发');await until(()=>frame.locator('.session').count().then(n=>n>=1),'mobile search');await frame.locator('.session[data-session-id="'+id+'"]').click();assert.equal(await frame.locator('#sidebar-toggle').getAttribute('aria-expanded'),'false');await frame.locator('#find').click();await frame.locator('#search-input').press('Escape');assert(await frame.locator('#search-bar').isHidden());}
   await page.locator('iframe[title="设备终端工作台"]').screenshot({path:path.join(out,'width-'+width+'.png')});checks.push({width,noOverflow:true});
  }
  assert.equal(errors.length,0);fs.writeFileSync(path.join(out,'layout-result.json'),JSON.stringify({status:'PASS',browser:await browser.version(),checks,errors},null,2)+'\n');console.log('PASS: 4 themes, contrast, 3 viewport sizes, mobile search and Escape');
 }finally{
  if(id&&frame){try{if(await frame.locator('#close-session').isEnabled()){await frame.locator('#close-session').click();await frame.locator('#confirm-accept').click();await until(()=>frame.locator('#close-session').isDisabled(),'cleanup');}await frame.locator('#delete-session').click();await frame.locator('#confirm-accept').click();await until(()=>frame.locator('.session[data-session-id="'+id+'"]').count().then(n=>n===0),'remove layout test session');}catch(e){console.error('Session cleanup: '+e.message);}}
  if(page&&!page.isClosed()){const close=page.getByRole('button',{name:'关闭工作台',exact:true});if(await close.isVisible().catch(()=>false))await close.click().catch(()=>{});}
  await browser.close();
 }
})().catch(e=>{console.error(e.message);process.exitCode=1;});
