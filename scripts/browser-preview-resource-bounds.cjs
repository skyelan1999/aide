'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const {chromium} = require('./browser-runtime/node_modules/playwright');
const {source: dxfSource} = require('./test_dxf_preview_bounds.cjs');
const {docxSource, zip, sizes} = require('./test_docx_preview_bounds.cjs');
async function main() {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert.ok(out, 'set owned AIDE_COMPONENT_EVIDENCE_DIR');fs.mkdirSync(out, {recursive: true});
  const normal = await zip({
    '[Content_Types].xml': '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>',
    '_rels/.rels': '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>',
    'word/document.xml': '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Owned normal DOCX preview</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="720" w:right="720" w:bottom="720" w:left="720"/></w:sectPr></w:body></w:document>'
  });
  const line = ['0', 'LINE', '8', '0', '10', '0', '20', '0', '11', '10', '21', '10'];
  const text = value => ['0','TEXT','8','0','10','1','20','3','40','1','1',value];
  const wrapDxf = (blocks, entities) => ['0','SECTION','2','HEADER','0','ENDSEC','0','SECTION','2','BLOCKS',...blocks,'0','ENDSEC','0','SECTION','2','ENTITIES',...entities,'0','ENDSEC','0','EOF',''].join('\n');
  const cyclicBlock = ['0','BLOCK','2','loop','70','0','10','0','20','0','3','loop','1','','0','INSERT','2','loop','10','0','20','0','0','ENDBLK'];
  const fixtures = new Map([
    ['normal.dxf', Buffer.from(wrapDxf([], [...line, ...text('Owned DXF label')]))],
    ['cycle.dxf', Buffer.from(wrapDxf(cyclicBlock, ['0','INSERT','2','loop','10','0','20','0']))],
    ['textloop.dxf', Buffer.from(wrapDxf(['0','BLOCK','2','label','70','0','10','0','20','0','3','label','1','',...text('x'.repeat(10000)),'0','ENDBLK'],
      Array.from({length:110},()=>['0','INSERT','2','label','10','0','20','0']).flat()))],
    ['normal.docx', Buffer.from(normal)], ['declared.docx', Buffer.from(sizes(normal, 64 * 1024 * 1024))],
    ['lying.docx', Buffer.from(sizes(await zip({'word/document.xml': new Uint8Array(32 * 1024 * 1024)}), 1024))]
  ]);
  const browser = await chromium.launch({executablePath: '/usr/bin/chromium', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage']});
  const errors = [], checks = [];let context;
  try {
    context = await browser.newContext();const page = await context.newPage();page.on('pageerror', error => errors.push(error.message));
    await page.route('https://aide-preview-owned.test/**', route => {
      const url = new URL(route.request().url());
      if (url.pathname === '/') return route.fulfill({contentType: 'text/html; charset=utf-8', body: '<!doctype html><meta charset="utf-8"><title>Owned preview fixtures</title><body style="display:block;padding:16px"><h1>组件验收 · 模拟文件</h1><main id="preview" style="height:800px;overflow:auto"></main>'});
      const bytes = fixtures.get(url.pathname.split('/').pop());
      return route.fulfill(bytes ? {contentType: 'application/octet-stream', body: bytes} : {status: 404, body: ''});
    });
    await page.goto('https://aide-preview-owned.test/', {waitUntil: 'load'});
    await page.addStyleTag({content: ['themes/dark/tokens.css', 'themes/light/tokens.css', 'style.css', 'macos.css', 'workbench.css'].map(file => fs.readFileSync('internal/server/web/' + file, 'utf8')).join('\n')});
    for (const file of ['vendor/jszip.min.js', 'vendor/docx-preview/docx-preview.min.js', 'vendor/dxf-parser/dxf-parser.js', 'preview-safety.js', 'docx-style-bridge.js']) await page.addScriptTag({content: fs.readFileSync('internal/server/web/' + file, 'utf8')});
    await page.evaluate(() => {
      window.t = value => value;window.escapeHtml = value => String(value).replace(/[&<>]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]));
      window.el = (tag, cls = '', text = '') => {const node = document.createElement(tag);node.className = cls;node.textContent = text;return node;};
      window.ensureDxfParser = async () => {};window.fileRawUrl = name => '/fixture/' + name;
      window.isDocPath = () => false;window.isBackupPath = () => false;
      window.ownedTeardowns = [];
      window.ownedRenderCalls = 0;const render = window.docx.renderAsync;
      window.docx.renderAsync = (...args) => {ownedRenderCalls++;return render(...args);};
    });
    const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
    const lifecycle = app.slice(app.indexOf('function teardownViewer('), app.indexOf('async function setupStlPreview('));
    assert.ok(lifecycle.includes('function bindViewerTeardown('), 'exercise current product lifecycle helpers');
    await page.addScriptTag({content: lifecycle + '\n' + dxfSource() + '\n' + docxSource()});
    await page.evaluate(() => {
      const bind = window.bindViewerTeardown;
      window.bindViewerTeardown = (host, release) => {bind(host, release);ownedTeardowns.push(host._aideViewerTeardown);};
    });
    for (const [theme, width] of [['light',1440], ['dark',390]]) {
      await page.setViewportSize({width, height: 1000});await page.evaluate(theme => {document.documentElement.dataset.theme = theme;}, theme);
      await page.evaluate(async () => {for (const fn of ownedTeardowns.splice(0)) fn();await setupDxfPreview(document.querySelector('#preview'), 'normal.dxf', 'workspace', '');});
      assert.equal(await page.locator('#preview svg line').count(), 1);
      await page.getByText('Owned DXF label', {exact:true}).waitFor();
      const before = await page.locator('#preview svg').getAttribute('viewBox');
      await page.locator('.dxf-canvas').hover();await page.mouse.wheel(0,-120);
      await page.waitForFunction(before => document.querySelector('#preview svg').getAttribute('viewBox') !== before, before);
      await page.screenshot({path: path.join(out, theme + '-dxf.png')});
      await page.evaluate(async () => {for (const fn of ownedTeardowns.splice(0)) fn();await setupDxfPreview(document.querySelector('#preview'), 'cycle.dxf', 'workspace', '');});
      assert.equal(await page.locator('#preview svg').count(), 0);assert.match(await page.locator('.dxf-error').textContent(), /循环|层级/);
      await page.evaluate(async () => {await setupDxfPreview(document.querySelector('#preview'),'textloop.dxf','workspace','');});
      assert.equal(await page.locator('#preview svg').count(),0);assert.match(await page.locator('.dxf-error').textContent(),/预览限制/);
      await page.evaluate(async () => {await setupDocxPreview(document.querySelector('#preview'), 'normal.docx', 'workspace', '', {readOnly:true,origin:'owned'});});
      assert.equal(await page.locator('.docx-error').count(), 0, await page.locator('#preview').textContent());
      await page.getByText('Owned normal DOCX preview', {exact:true}).waitFor();
      const accepted = await page.evaluate(() => ownedRenderCalls);
      await page.screenshot({path: path.join(out, theme + '-docx.png')});
      const rejected = [];
      for (const name of ['declared.docx', 'lying.docx']) {
        const elapsed = await page.evaluate(async name => {const start = performance.now();await setupDocxPreview(document.querySelector('#preview'),name,'workspace','',{readOnly:true,origin:'owned'});return performance.now()-start;}, name);
        assert.match(await page.locator('#preview').textContent(), /预览限制/);assert.equal(await page.locator('.docx-body').count(),0);
        assert.equal(await page.evaluate(() => ownedRenderCalls), accepted);rejected.push({name, elapsedMs: elapsed});
      }
      await page.screenshot({path: path.join(out, theme + '-rejected.png')});checks.push({theme,width,result:'PASS',rejected});
    }
    assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({checks,errors,fullWorkbench:false,realVendorLibraries:true},null,2)+'\n');
    console.log(JSON.stringify(checks));
  } finally {if (context) await context.close();await browser.close();}
}
main().catch(error => {console.error(error.message);process.exitCode=1;});
