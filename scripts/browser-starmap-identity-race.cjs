'use strict';
// Shipped starmap page, mocked API/lock boundary. No full workbench/server claim.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const {chromium} = require('./browser-runtime/node_modules/playwright');
async function main() {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert.ok(out, 'set an owned AIDE_COMPONENT_EVIDENCE_DIR');fs.mkdirSync(out, {recursive: true});
  const web = path.resolve('internal/server/web'), errors = [], checks = [];
  const browser = await chromium.launch({executablePath: '/usr/bin/chromium', headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage']});
  let context, page;
  try {
    context = await browser.newContext();page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    let mismatch = false, switched = false, release;
    const node = {id: 'owned-A', kind: 'file', name: 'Owned A fixture', path: 'owned-A.txt', region: 'workspace',
      root: 'workspace', text: 'private A fixture detail', size: 25};
    await page.route('https://aide-owned.test/**', async route => {
      const url = new URL(route.request().url());
      const json = value => route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(value)});
      if (url.pathname.startsWith('/api/')) {
        if (url.pathname.endsWith('/identity')) return json({workspace: switched ? 'B' : 'A', epoch: 'owned', policy: 'owned'});
        if (url.pathname.endsWith('/updates')) {
          if (mismatch && !switched) {switched = true;return json({workspace: 'B', revision: 'B-unverified', reset: true, nodes: [], edges: []});}
          if (switched) {await new Promise(resolve => {release = resolve;});return json({workspace: 'B', revision: 'B2', reset: true, nodes: [], edges: [], sources: [], warnings: []});}
          return json({workspace: 'A', revision: 'A1', reset: true, nodes: [node], edges: [], sources: [], warnings: []});
        }
        return json({});
      }
      if (url.pathname === '/lock-cluster.js') return route.fulfill({contentType: 'text/javascript', body: '/* owned unlocked boundary fixture */'});
      const target = path.resolve(web, '.' + url.pathname);
      if (!target.startsWith(web + path.sep) || !fs.existsSync(target) || !fs.statSync(target).isFile()) return route.fulfill({status: 404, body: ''});
      const contentType = {'.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml'}[path.extname(target)] || 'application/octet-stream';
      return route.fulfill({contentType, body: fs.readFileSync(target)});
    });
    await page.addInitScript(() => {localStorage.setItem('aide-token', 'owned-fixture-token');});
    for (const [theme, width] of [['light', 1440], ['dark', 390]]) {
      mismatch = switched = false;release = null;
      await page.setViewportSize({width, height: 1000});
      await page.goto('https://aide-owned.test/starmap.html', {waitUntil: 'load'});
      await page.waitForFunction(() => !loading && liveCursor === 'A1' && nodeByID.has('owned-A'));
      await page.evaluate(theme => {document.documentElement.dataset.theme = theme;stopLive(true);}, theme);
      await page.locator('#results button.result').click();
      await page.waitForFunction(() => selected?.id === 'owned-A' && !$('detail').hidden);
      await page.evaluate(() => {
        window.AideStarmapCache.read = async () => null;
        $('search').value = 'private A query';$('question').value = 'private A question';
        $('answer').textContent = 'private A answer';$('usage').textContent = 'private A usage';
        $('ai-found').append(document.createTextNode('private A projection'));
        window.ownedAIJob = aiExplorer.start('owned pending AI');
        pendingRecoveredView = {version: 1, query: 'private A recovered'};
      });
      mismatch = true;
      await page.evaluate(() => {void load();});
      await page.waitForFunction(() => graph.workspace === 'B' && nodeByID.size === 0 && loading);
      const state = await page.evaluate(() => ({selected, nodes: nodeByID.size, rendered: renderNodes.length,
        results: $('results').children.length, detailHidden: $('detail').hidden, answer: $('answer').textContent,
        usage: $('usage').textContent, projection: $('ai-found').textContent, query: $('search').value,
        question: $('question').value, recovery: pendingRecoveredView, cursor: liveCursor,
        detailText: $('detail-text').textContent, fading: !!graphFade,
        aiAborted: window.ownedAIJob.controller.signal.aborted}));
      assert.equal(state.selected, null);assert.equal(state.nodes, 0);assert.equal(state.rendered, 0);
      assert.equal(state.results, 0);assert.equal(state.detailHidden, true);
      assert.equal(state.detailText, '');assert.equal(state.fading, false);
      for (const key of ['usage', 'projection', 'query', 'question', 'cursor']) assert.equal(state[key], '', key);
      assert.ok(!state.answer.includes('private A'));assert.equal(state.recovery, null);assert.equal(state.aiAborted, true);
      await page.screenshot({path: path.join(out, theme + '-invalidated.png')});
      assert.equal(typeof release, 'function', 'new identity must trigger immediate reload');release();
      await page.waitForFunction(() => !loading && liveCursor === 'B2');
      await page.evaluate(() => {stopLive(true);});
      checks.push({theme, width, result: 'PASS', state});
    }
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(out, 'result.json'), JSON.stringify({checks, errors, mockedAPI: true, mockedLockBoundary: true, fullWorkbench: false}, null, 2) + '\n');
    console.log(JSON.stringify(checks));
  } catch (error) {
    fs.writeFileSync(path.join(out, 'failure.json'), JSON.stringify({errors, message: error.message,
      state: await page?.evaluate(() => ({status: document.querySelector('#status')?.textContent,
        body: document.body.textContent.slice(-1800), scripts: [...document.scripts].map(script => script.src)})).catch(() => null)}, null, 2));
    throw error;
  } finally {
    if (context) await context.close();await browser.close();
  }
}
main().catch(error => {console.error(error);process.exitCode = 1;});
