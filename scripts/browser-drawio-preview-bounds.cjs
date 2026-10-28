'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const {chromium} = require('./browser-runtime/node_modules/playwright');
const {compressed} = require('./test_drawio_preview_bounds.cjs');
const web = path.resolve('internal/server/web');
const app = fs.readFileSync(path.join(web, 'app.js'), 'utf8');
const setup = app.slice(app.indexOf('function setupDrawioFrame('), app.indexOf('/* 图片预览器：'));
const model = '<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="Owned draw.io label" vertex="1" parent="1"><mxGeometry x="40" y="40" width="180" height="60" as="geometry"/></mxCell></root></mxGraphModel>';
async function main() {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert.ok(out);fs.mkdirSync(out, {recursive:true});
  const browser = await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
  const errors = [], checks = [], aiRequests = [];let context;
  try {
    context = await browser.newContext();const page = await context.newPage();
    page.on('pageerror', error => {errors.push(error.message);fs.appendFileSync(path.join(out,'page-errors.log'),error.stack+'\n');});
    await context.addInitScript(() => {
      if (window === window.parent) return;
      let safety;
      Object.defineProperty(window, 'AideDrawioSafety', {configurable:true, get:()=>safety, set:value=> {
        safety = value;const install = value.install;
        value.install = function () {
          const result = install.apply(this, arguments);
          if (!window.ownedObserved) {
            window.ownedObserved = true;window.ownedInstalledBeforeMain = !App.isMainCalled;
            window.ownedTrelloClass = window.TrelloClient;
            const set = Editor.prototype.setGraphXml;
            Editor.prototype.setGraphXml = function () {window.ownedEditor = this;return set.apply(this, arguments);};
            const data = EditorUi.prototype.setFileData;
            EditorUi.prototype.setFileData = function () {window.ownedUi = this;return data.apply(this, arguments);};
          }
          return result;
        };
      }});
    });
    await context.route('**/*', async route => {
      const url = new URL(route.request().url());
      if (url.origin !== 'https://aide-drawio-owned.test') return route.abort();
      if(process.env.AIDE_DRAWIO_AI_ACCEPTANCE&&url.pathname==='/owned-ai-template'){
        aiRequests.push(JSON.parse(route.request().postData()));
        return route.fulfill({contentType:'application/json',body:JSON.stringify({choices:[{message:{content:'Owned mock answer '+aiRequests.length}}]})});
      }
      if (url.pathname === '/') return route.fulfill({contentType:'text/html',body:'<!doctype html><meta charset="utf-8"><title>Owned draw.io component</title><body style="margin:0;background:#fff;color:#333"><main id="preview" style="height:95vh"></main>'});
      const file = path.resolve(web, '.' + decodeURIComponent(url.pathname) + (url.pathname.endsWith('/')?'index.html':''));
      if (!file.startsWith(web + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) return route.fulfill({status:404,body:''});
      const ext = path.extname(file), type = {'.js':'application/javascript','.html':'text/html','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.gif':'image/gif','.xml':'application/xml','.txt':'text/plain'}[ext] || 'application/octet-stream';
      return route.fulfill({contentType:type,body:fs.readFileSync(file)});
    });
    await page.goto('https://aide-drawio-owned.test/');
    await page.evaluate(() => {window.t = value => value;window.ownedSaves = [];});
    await page.addScriptTag({content:setup});
    async function open(xml) {
      await page.evaluate(xml => setupDrawioFrame(document.querySelector('#preview'), xml, value=>ownedSaves.push(value), 'ownedDrawioHandler'), xml);
    }
    for (const [theme,width] of [['light',1440],['dark',390]]) {
      await page.setViewportSize({width,height:1000});
      await open('<mxfile><diagram id="page1">' + compressed(model) + '</diagram><diagram id="page2">' + compressed(model) + '</diagram></mxfile>');
      const handle = await page.locator('#preview iframe').elementHandle();
      const frame = await handle.contentFrame();await handle.dispose();
      await frame.waitForFunction(()=>window.ownedEditor && ownedEditor.graph.model.getCell('2'),{},{timeout:30000});
      assert.equal(await frame.evaluate(()=>AideDrawioSafety.installed && ownedInstalledBeforeMain),true);
      await page.evaluate(() => {
        const source = document.querySelector('iframe').contentWindow;
        for (const [origin, from, data] of [
          ['https://untrusted.invalid', source, {event:'aide-preview-error'}],
          [location.origin, window, {event:'aide-preview-error'}],
          ['https://untrusted.invalid', source, {event:'save',xml:'untrusted'}],
          [location.origin, source, null]
        ]) window.dispatchEvent(new MessageEvent('message',{origin,source:from,data:JSON.stringify(data)}));
      });
      assert.equal(await page.locator('#preview iframe').count(),1);
      assert.deepEqual(await page.evaluate(()=>ownedSaves),[]);
      if (theme === 'dark') await frame.evaluate(()=>ownedUi.setDarkMode(true));
      // Interact with the actual graph editor, then use its actual Save button/protocol.
      await frame.evaluate(()=>{ownedEditor.graph.setSelectionCell(ownedEditor.graph.model.getCell('2'));ownedEditor.graph.startEditingAtCell();});
      const input = frame.locator('.mxCellEditor');await input.waitFor();await input.fill('Edited owned label');await input.press('Tab');
      await frame.getByText('Save', {exact:true}).last().click();
      await page.waitForFunction(()=>ownedSaves.length > 0);
      const saved = await page.evaluate(()=>ownedSaves.pop());assert.match(saved, /mxfile|mxGraphModel/);
      assert.equal(await frame.evaluate(()=>ownedEditor.graph.model.getCell('2').value.trim()), 'Edited owned label');
      await page.screenshot({path:path.join(out,theme+'-drawio.png')});
      checks.push({case:'normal compressed multi-page, edit and save',theme,width,result:'PASS'});
      if (process.env.AIDE_DRAWIO_TRELLO_ACCEPTANCE) checks.push(...await require('./drawio-trello-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_VISIO_ACCEPTANCE) checks.push(...await require('./drawio-visio-archive-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_LUCID_ACCEPTANCE) checks.push(...await require('./drawio-lucid-message-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_ANIMATION_ACCEPTANCE) checks.push(...await require('./drawio-animation-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_ANIMATION_FOOTER_ACCEPTANCE) checks.push(...await require('./drawio-animation-footer-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_AI_ACCEPTANCE) checks.push(...await require('./drawio-ai-template-browser-cases.cjs')(frame,out,theme,aiRequests));
      if (process.env.AIDE_DRAWIO_LIFECYCLE_ACCEPTANCE) checks.push(...await require('./drawio-lifecycle-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_GEOMETRY_ACCEPTANCE) checks.push(...await require('./drawio-geometry-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_CLONE_CONSTRAINT_ACCEPTANCE) checks.push(...await require('./drawio-clone-constraint-browser-cases.cjs')(frame,theme));
      if (process.env.AIDE_DRAWIO_LAYOUT_ACCEPTANCE) checks.push(...await require('./drawio-layout-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_GIF_ACCEPTANCE) checks.push(...await require('./drawio-gif-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_FREEHAND_ACCEPTANCE) checks.push(...await require('./drawio-freehand-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_SDK_REVIEW) checks.push(...await require('./drawio-sdk-review-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_PALETTE_SEARCH_ACCEPTANCE) checks.push(...await require('./drawio-palette-search-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_AWS_SEARCH_ACCEPTANCE) checks.push(...await require('./drawio-aws-search-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_SIDEBAR_METADATA_ACCEPTANCE) checks.push(...await require('./drawio-sidebar-metadata-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_STATE_SEARCH_ACCEPTANCE) checks.push(...await require('./drawio-state-search-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_BPMN_PALETTE_ACCEPTANCE) checks.push(...await require('./drawio-bpmn-palette-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_BOOTSTRAP_ACCEPTANCE) checks.push(...await require('./drawio-bootstrap-palette-browser-cases.cjs')(frame,out,theme));
      if (process.env.AIDE_DRAWIO_CAPTION_METADATA_ACCEPTANCE) checks.push(...await require('./drawio-caption-metadata-browser-cases.cjs')(frame,out,theme));
    }
    const flat = '<mxGraphModel><root>' + Array.from({length:129},(_,n)=>{const i=128-n;return '<mxCell id="'+i+'"'+(i?' parent="'+(i-1)+'"':'')+'/>';}).join('') + '</root></mxGraphModel>';
    const largePage = compressed('<mxGraphModel><!--'+'x'.repeat(6*1024*1024)+'--><root><mxCell id="0"/></root></mxGraphModel>');
    const lazy = '<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/>' +
      Array.from({length:140},(_,n)=>'<mxCell id="'+(n+2)+'" parent="1"'+(n<139?' source="'+(n+3)+'"':'')+'/>').join('')+'</root></mxGraphModel>';
    for (const [name,xml] of [
      ['expanded', '<mxfile><diagram>'+compressed('x'.repeat(8*1024*1024+1))+'</diagram></mxfile>'],
      ['xml-depth','<n>'.repeat(129)+'</n>'.repeat(129)],
      ['cell-depth', flat],
      ['cell-cycle','<mxGraphModel><root><mxCell id="a" parent="b"/><mxCell id="b" parent="a"/></root></mxGraphModel>'],
      ['inactive-page', '<mxfile><diagram>'+compressed(model)+'</diagram><diagram>'+compressed(flat)+'</diagram></mxfile>'],
      ['page-count','<mxfile>'+'<diagram/>'.repeat(65)+'</mxfile>'],
      ['aggregate-expansion','<mxfile>'+('<diagram>'+largePage+'</diagram>').repeat(3)+'</mxfile>'],
      ['lazy-reference-depth',lazy],
      ...(process.env.AIDE_DRAWIO_LUCID_ACCEPTANCE ? [['untrusted-stencil-shaped-xml',require('./drawio-lucid-message-browser-cases.cjs').cachedAsset('aws4.xml')]] : [])
    ]) {
      const start = Date.now();await open(xml);
      await page.getByText('draw.io 内容超过安全预览限制，请下载后查看',{exact:true}).waitFor({timeout:30000});
      assert.equal(await page.locator('#preview iframe').count(),0);
      checks.push({case:name,result:'PASS',elapsedMs:Date.now()-start});
    }
    await page.screenshot({path:path.join(out,'rejected.png')});
    assert.ok(errors.every(error=>error === 'draw.io preview resource limit exceeded'), JSON.stringify(errors));
    fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({checks,errors,browser:browser.version(),actualBundledIframe:true,fullWorkbench:false},null,2)+'\n');
    console.log(JSON.stringify(checks));
  } finally {if(context) await context.close();await browser.close();}
}
main().catch(error=>{console.error(error.stack);process.exitCode=1;});
