'use strict';
const assert = require('node:assert/strict');
const {execFileSync} = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const {chromium} = require('./browser-runtime/node_modules/playwright');

const root = path.resolve(__dirname, '..');
const fixture = execFileSync('python3', ['scripts/fixtures/render-context-accounting.py'], {cwd: root, encoding: 'utf8'}).trim();
const english = fs.readFileSync(path.join(root, 'internal/server/web/locales/en.js'), 'utf8');

async function checkLayout(browser, mobile) {
  const context = await browser.newContext({
    viewport: mobile ? {width: 380, height: 650} : {width: 1360, height: 870},
    isMobile: mobile, hasTouch: mobile,
  });
  try {
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(String(error)));
    await page.goto(pathToFileURL(fixture).href);
    const rings = page.locator('#context-parallel-rings .usage-ring');
    assert.equal(await rings.count(), 10);
    if (mobile) await rings.nth(9).tap();
    else await rings.nth(9).hover();
    const tip = page.locator('#context-ring-tip');
    assert.match(await tip.innerText(), /未分配执行环境/);
    await rings.nth(1).focus();
    assert.match(await tip.innerText(), /示例任务 A.*窗口占用/);
    await page.keyboard.press('Escape');
    assert.equal(await tip.isVisible(), false);
    await page.locator('#context-toggle').focus();
    await page.keyboard.press('Enter');
    await page.locator('#context-usage-dialog').waitFor();
    await page.locator('#context-environments summary').click();
    assert.equal(await page.locator('#context-environment-list li').count(), 3);
    await page.keyboard.press('Escape');
    assert.equal(await page.evaluate(() => document.activeElement.id), 'context-toggle');

    await page.addScriptTag({content: english});
    await page.evaluate(() => {
      window.aideI18n = {t: (key, ...args) => (window.aideEnglish[key] || key).replace(/\{(\d+)\}/g, (_, i) => args[i] ?? '')};
      for (const node of document.querySelectorAll('#context-card [data-i18n]')) node.textContent = t(node.dataset.i18n);
      delete state.session.sessionUsage;
      const run = state.session.runs[0];
      run.usage = {prompt: 22000000, completion: 200000, estimated: true};
      run.created = '2026-11-01T00:00:00Z';
      run.orchestrationEvents = [{completedAt: '2026-11-05T04:01:05Z'}];
      renderContextMeter({...preview, inputEstimate: 78644}, false);
    });
    assert.equal(await page.locator('#context-card-label').innerText(), 'Latest task usage');
    assert.equal(await page.locator('#context-usage-kind').innerText(), 'Includes estimates');
    assert.equal(await page.locator('#context-budget-value').count(), 0);
    assert.equal(await page.locator('#context-budget-detail').count(), 0);
    for (const width of [180, 210, 260]) {
      const layout = await page.evaluate(width => {
        document.querySelector('.fixture-preview').style.width = width + 'px';
        const header = document.querySelector('#context-toggle');
        return {available: header.clientWidth, used: header.scrollWidth,
                rings: [...document.querySelectorAll('.usage-ring-face')].map(ring => ring.offsetWidth)};
      }, width);
      assert.ok(layout.used <= layout.available, 'English usage and runtime exceed a ' + width + 'px sidebar');
      assert.deepEqual(layout.rings, Array(10).fill(14));
    }
    await page.locator('#legacy').click();
    assert.equal(await rings.count(), 5);
    await page.locator('#ordinary').click();
    assert.equal(await rings.count(), 1);
    assert.deepEqual(errors, []);
  } finally {
    await context.close();
  }
}
(async () => {
  const browser = await chromium.launch(process.env.CHROMIUM_PATH ? {executablePath: process.env.CHROMIUM_PATH} : {channel: 'chrome'});
  try {
    await checkLayout(browser, false);
    await checkLayout(browser, true);
    console.log('PASS: desktop and touch context rings, keyboard access, slot capacity and narrow English headers');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
