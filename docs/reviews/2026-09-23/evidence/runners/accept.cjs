'use strict';
/* aide 浏览器级补验（DSH 实施）：真实 Chromium + 隔离候选 + 循环 Mock。
 * 覆盖：费用可见/0价/按模型持久化、热力图悬浮与单日明细、上下文预览、
 * 旧文件标签跨工作区拒绝误写、会话切换草稿隔离。输出断言、截图、控制台。 */
const { chromium } = require('playwright-core');
const fs = require('node:fs');
const path = require('node:path');

const AIDE_URL = process.env.AIDE_URL || 'http://127.0.0.1:18097';
const MOCK_LOG_URL = process.env.MOCK_LOG_URL || 'http://127.0.0.1:19091/log';
const TOKEN = fs.readFileSync(process.env.TOKEN_FILE, 'utf-8').trim();
const EVIDENCE = process.env.EVIDENCE || '/private/tmp/aide-independent-acceptance/frontend/browser-harness/evidence';

const results = [];
const consoleLog = [];
function record(label, status, detail) {
  results.push({ check: label, status, detail });
  console.log(JSON.stringify({ check: label, status, detail }));
}
function assert(cond, msg) { if (!cond) throw new Error(msg); }

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1500, height: 950 } });
  const page = await context.newPage();
  page.on('console', msg => consoleLog.push({ type: msg.type(), text: msg.text() }));
  page.on('pageerror', err => consoleLog.push({ type: 'pageerror', text: String(err) }));
  const shot = name => page.screenshot({ path: path.join(EVIDENCE, name), fullPage: false });
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const api = (method, p, body) => page.evaluate(async ([m, p2, b]) => {
    const r = await fetch('/api' + p2, { method: m, headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + localStorage.getItem('aide-token') }, body: b ? JSON.stringify(b) : undefined });
    return { status: r.status, body: await r.json().catch(() => null) };
  }, [method, p, body || null]);

  fs.mkdirSync(EVIDENCE, { recursive: true });
  try {
    // ── 0) 加载与鉴权 ──
    await page.goto(AIDE_URL + '/?token=' + TOKEN, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => document.querySelector('#connection')?.textContent.includes('本地服务已连接'), null, { timeout: 15000 });
    await shot('00-loaded.png');
    record('browser-00-load', 'PASS', { url: AIDE_URL, title: await page.title() });

    // ── 1) 一次对话任务（记录调用，供费用/预览断言）──
    await page.fill('#prompt', 'TOOL_PROBE 请查看目录');
    await page.waitForFunction(() => !document.querySelector('#context-preview').classList.contains('hidden'), null, { timeout: 10000 });
    await shot('01-preview-visible.png');
    const cpText = await page.textContent('#cp-summary');
    record('browser-01-preview-visible', /输入估算 \d+ tokens/.test(cpText) ? 'PASS' : 'FAIL', { cpSummary: cpText });
    await page.click('#cp-toggle');
    await shot('01b-preview-detail.png');
    const cpRows = await page.$$eval('#cp-detail .cp-row', rows => rows.length);
    assert(cpRows >= 4, 'preview breakdown rows missing: ' + cpRows);
    record('browser-01-preview-breakdown', 'PASS', { rows: cpRows, note: await page.textContent('#cp-detail .cp-note') });
    await page.click('#send');
    await page.waitForFunction(() => {
      const runs = JSON.parse(localStorage.getItem('aide-token') || 'null');
      return true;
    }).catch(() => {});
    await page.waitForFunction(() => document.querySelector('#timeline')?.textContent.includes('验收响应'), null, { timeout: 20000 });
    await shot('01c-task-done.png');
    // 预览估算与真实请求一致性：从 mock 捕获的首个主请求重算组成
    const logResp = await fetch(MOCK_LOG_URL);
    const log = await logResp.json();
    const mainReqs = log.filter(r => Array.isArray(r.tools) && r.tools.length);
    assert(mainReqs.length >= 2, 'mock did not capture tool-bearing main requests: ' + log.length);
    const first = mainReqs[0];
    const msgs = first.messages;
    const utf8 = s => Buffer.byteLength(s, 'utf8');
    const sumChars = msgs.reduce((a, m) => a + utf8(String(m.content || '')), 0);
    const bodyObj = await page.evaluate(() => {
      const el = document.querySelector('#cp-summary');
      return el ? el.textContent : '';
    });
    const est = Number((bodyObj.match(/输入估算 (\d+)/) || [])[1]);
    assert(Number.isFinite(est), 'cannot parse preview estimate');
    assert(est === Math.floor(sumChars / 4), 'preview estimate not derived from real request chars: ' + est + ' vs ' + Math.floor(sumChars / 4));
    record('browser-01-preview-estimate-consistent', 'PASS', { estimate: est, realRequestChars: sumChars, mainRequests: mainReqs.length });

    // ── 2) 费用可见 + 0 价 + 刷新持久化 + 热力图/单日明细 ──
    await page.click('#brand-button');
    await page.waitForSelector('#settings-sheet.open');
    await page.click('#settings-nav button:has-text("消耗统计")');
    await page.waitForSelector('.token-stats .token-cell');
    await page.waitForFunction(() => document.querySelector('.token-stats .token-head .control-value')?.textContent.includes('已计价费用'));
    const headText = await page.textContent('.token-stats .token-head .control-value');
    record('browser-02-cost-visible', headText.includes('已计价费用 ¥') ? 'PASS' : 'FAIL', { head: headText });
    // 0 价：输入与输出单价置 0（0 是合法免费，不等于留空）
    const priceIn = page.locator('input[aria-label="输入 ¥/百万"]');
    const priceOut = page.locator('input[aria-label="输出 ¥/百万"]');
    await priceIn.fill('0');
    await priceIn.dispatchEvent('change');
    await priceOut.fill('0');
    await priceOut.dispatchEvent('change');
    await sleep(500);
    let pricing = await api('GET', '/token-pricing');
    assert(pricing.body.priceIn === 0 && pricing.body.priceOut === 0, '0 price not saved: ' + JSON.stringify(pricing.body));
    record('browser-02-zero-price-saved', 'PASS', { pricing: pricing.body });
    await shot('02-zero-price.png');
    // 热力图悬浮与单日明细
    const cells = page.locator('.token-cell:not(.future)');
    const todayCell = cells.last();
    await todayCell.hover();
    await sleep(300);
    const tipVisible = await page.evaluate(() => !document.querySelector('#token-tip')?.classList.contains('show') ? false : (document.querySelector('#token-tip').textContent || '').includes('tokens'));
    assert(tipVisible, 'heatmap tooltip not shown on hover');
    await shot('02-heatmap-tooltip.png');
    await todayCell.click();
    await sleep(200);
    const dayDetail = await page.evaluate(() => { const d = document.querySelector('#token-day-detail'); return d && !d.classList.contains('hidden') ? d.textContent : ''; });
    assert(dayDetail.includes('调用') && dayDetail.includes('tokens'), 'day detail not shown: ' + dayDetail);
    record('browser-02-heatmap-tooltip-day-detail', 'PASS', { tip: true, dayDetail: dayDetail.slice(0, 80) });
    await shot('02-day-detail.png');
    // 刷新后 0 价仍 0
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => document.querySelector('#connection')?.textContent.includes('本地服务已连接'), null, { timeout: 15000 });
    pricing = await api('GET', '/token-pricing');
    assert(pricing.body.priceIn === 0 && pricing.body.priceOut === 0, '0 price lost after reload');
    await page.click('#brand-button');
    await page.waitForSelector('#settings-sheet.open');
    await page.click('#settings-nav button:has-text("消耗统计")');
    await page.waitForSelector('.token-stats .token-cell');
    assert((await priceIn.inputValue()) === '0' && (await priceOut.inputValue()) === '0', '0 price inputs not persisted in UI');
    record('browser-02-zero-price-reload-stable', 'PASS', { pricing: pricing.body });
    // 第二模型费率：切换活动模型后单独保存 10/40，刷新持久化（按模型）
    await page.click('#settings-sheet-close');
    await page.click('#settings-button');
    await page.fill('#new-model-id', 'model-b');
    await page.click('#add-model');
    await page.locator('.model-row').nth(1).locator('.model-active').click();
    await page.click('#settings-form button[type="submit"], #settings-form .primary');
    await sleep(400);
    await page.click('#brand-button');
    await page.waitForSelector('#settings-sheet.open');
    await page.click('#settings-nav button:has-text("消耗统计")');
    await page.waitForSelector('.token-stats .token-cell');
    await priceIn.fill('10');
    await priceIn.dispatchEvent('change');
    await priceOut.fill('40');
    await priceOut.dispatchEvent('change');
    await sleep(500);
    pricing = await api('GET', '/token-pricing');
    const rates = pricing.body.rates || {};
    assert(rates['browser-model'] && rates['browser-model'].priceIn === 0, 'model rates lost: ' + JSON.stringify(rates));
    assert(rates['model-b'] && rates['model-b'].priceIn === 10 && rates['model-b'].priceOut === 40, 'model-b rate not saved: ' + JSON.stringify(rates));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => document.querySelector('#connection')?.textContent.includes('本地服务已连接'), null, { timeout: 15000 });
    pricing = await api('GET', '/token-pricing');
    assert((pricing.body.rates['model-b'] || {}).priceIn === 10, 'model-b rate lost after reload: ' + JSON.stringify(pricing.body.rates));
    record('browser-02-per-model-rates-persist', 'PASS', { rates: pricing.body.rates });
    await shot('02-model-b-rates.png');

    // ── 3) 旧文件标签跨工作区拒绝误写 ──
    await page.click('#workspace-config-button');
    await page.waitForSelector('#workspace-sheet.open');
    await page.fill('#ws-path', 'A');
    await page.click('#ws-save');
    await sleep(500);
    await page.click('#refresh-files');
    await sleep(400);
    await page.click('#files .file-row:has-text("same.txt"), #files [data-path="same.txt"]');
    await page.waitForSelector('#editor-dialog[open], #editor');
    await page.waitForFunction(() => (document.querySelector('#editor')?.value || '').includes('A-ONLY'), null, { timeout: 8000 });
    await page.click('#workspace-config-button');
    await page.waitForSelector('#workspace-sheet.open');
    await page.fill('#ws-path', 'B');
    await page.click('#ws-save');
    await sleep(500);
    await page.fill('#editor', 'edited while viewing A');
    await page.click('#save-file');
    await page.waitForFunction(() => document.querySelector('#toast')?.textContent.includes('工作区已切换'), null, { timeout: 8000 });
    const toastText = await page.textContent('#toast');
    record('browser-03-old-file-tab-refused', 'PASS', { toast: toastText });
    await shot('03-file-tab-refusal.png');

    // ── 4) 会话切换草稿隔离 ──
    await page.click('#new-session');
    await page.fill('#prompt', 'SLOW_MARKER 任务A：慢慢完成');
    await page.click('#send');
    await sleep(500); // 请求已发出，mock 拖 3 秒
    await page.click('#new-session');
    await page.fill('#prompt', 'B 会话的未发送草稿');
    await sleep(4000); // 等 A 完成
    const draft = await page.inputValue('#prompt');
    assert(draft === 'B 会话的未发送草稿', 'draft replaced after A completed: ' + draft);
    const title = await page.textContent('#session-title');
    assert(title.includes('开始新的探索'), 'selection stolen by A completion: ' + title);
    const errToasts = await page.evaluate(() => [...document.querySelectorAll('#toast')].map(t => t.textContent).filter(t => /Unexpected|undefined|is not a function/.test(t)));
    assert(errToasts.length === 0, 'runtime errors during draft isolation: ' + errToasts.join(';'));
    record('browser-04-session-switch-draft-isolated', 'PASS', { draft, title });
    await shot('04-draft-isolated.png');

    // 控制台错误检查
    const bad = consoleLog.filter(e => e.type === 'error' || e.type === 'pageerror');
    record('browser-05-console-clean', bad.length === 0 ? 'PASS' : 'FAIL', { consoleErrors: bad, totalConsole: consoleLog.length });
  } catch (error) {
    record('browser-FATAL', 'FAIL', { error: String(error && error.stack || error) });
    await shot('99-fatal.png').catch(() => {});
  } finally {
    fs.writeFileSync(path.join(EVIDENCE, 'console.json'), JSON.stringify(consoleLog, null, 2));
    fs.writeFileSync(path.join(EVIDENCE, 'results.json'), JSON.stringify({ browser: 'chromium', headless: true, aideURL: AIDE_URL, results }, null, 2));
    await browser.close();
  }
  const failed = results.filter(r => r.status !== 'PASS').length;
  console.log(JSON.stringify({ summary: results.length - failed + '/' + results.length + ' browser checks passed', passed: results.length - failed, failed }));
  process.exit(failed ? 1 : 0);
})().catch(err => { console.error(JSON.stringify({ status: 'HARNESS_ERROR', error: String(err.stack || err) })); process.exit(2); });
