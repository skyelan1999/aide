'use strict';
const { chromium } = require('./browser-runtime/node_modules/playwright');
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict');
const base = process.env.AIDE_TEST_URL, token = process.env.AIDE_TEST_TOKEN, out = process.env.AIDE_EVIDENCE_DIR;
if (!base || !token || !out) throw Error('Use the isolated project records Go fixture and an owned evidence directory');
const marker = Date.now().toString(36);
const report = { status: 'fail', scope: 'Actual Aide DOM, HTTP and persistence with a deterministic unpaid provider; no production data or deployment', cases: [] };
fs.mkdirSync(out, { recursive: true });
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
(async () => {
 const browser = await chromium.launch({ headless: true });
 try {
 let index = 0;
 for (const language of ['zh-CN', 'en']) for (const width of [1440, 390]) {
  const n = index++, row = { id: `${language}-${width}`, status: 'fail', checks: [] }; report.cases.push(row);
  const ctx = await browser.newContext({ viewport: { width, height: 1000 }, reducedMotion: 'reduce' });
  await ctx.addInitScript(({ token, language, width }) => { localStorage.setItem('aide-token', token); localStorage.setItem('aide.ui', JSON.stringify({ version: 1, language, palette: width === 390 ? 'comfort' : 'blue', theme: width === 390 ? 'dark' : 'light', motion: 'reduced', startupAnimation: 'off' })); }, { token, language, width });
  const page = await ctx.newPage(), errors = []; page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message));
  const auth = { Authorization: 'Bearer ' + token };
  async function call(method, endpoint, data) { const r = await ctx.request.fetch(base + '/api' + endpoint, { method, headers: auth, data }); assert.ok(r.ok(), `${method} ${endpoint}: ${r.status()} ${await r.text()}`); return r.json(); }
  async function board() {
   if (!await page.locator('#trajectory-sheet').evaluate(n => n.classList.contains('open'))) await page.locator('#trajectory-toggle').click();
   await page.locator('.traj-main-seg [data-view="outcome"]').click();
   const picker = page.locator('#traj-outcome-run'); if (await picker.count()) await picker.selectOption('');
   await page.locator('.plan-add').waitFor();
  }
  try {
   await page.goto(base, { waitUntil: 'networkidle' });
   if (width === 390) await page.locator('#sidebar-toggle').click();
   await page.locator(`[data-session-id="project-records-${n}"]`).click();
   if (width === 390 && await page.locator('#sidebar-scrim').isVisible()) await page.locator('#sidebar-scrim').click({ position: { x: width - 8, y: 80 } });
   await board();
   const add = page.locator('.plan-add'); await add.locator('.plan-add-input').fill(`Goal fixture ${n} ${marker}`);
   await add.locator('.plan-contract-fields > summary').click();
   await add.locator('[name="deliverables"]').fill('Reviewed result');
   await add.locator('[name="criteria"]').fill('Fixture result inspected\nNo missing deliverables');
   // Reopening the parent view while input is unsaved reproduces the same
   // remount that initial session refresh can cause. No network stub is used.
   await page.locator('.traj-main-seg [data-view="history"]').click(); await board();
   assert.equal(await add.locator('.plan-add-input').inputValue(), `Goal fixture ${n} ${marker}`);
   assert.equal(await add.locator('[name="deliverables"]').inputValue(), 'Reviewed result');
   assert.equal(await add.locator('[name="criteria"]').inputValue(), 'Fixture result inspected\nNo missing deliverables');
   const creation = page.waitForResponse(r => r.url().endsWith(`/api/sessions/project-records-${n}/plan`) && r.request().method() === 'POST');
   await add.locator('button[type="submit"]').click();
   const creationResponse = await creation; assert.equal(creationResponse.status(), 201, await creationResponse.text());

   await page.locator('.plan-row').filter({ has: page.locator('.plan-title', { hasText: `Goal fixture ${n} ${marker}` }) }).waitFor();
   let s = await call('GET', `/sessions/project-records-${n}`); let item = s.plan.find(p => p.title === `Goal fixture ${n} ${marker}`); assert.ok(item && item.contract.criteria.length === 2); row.checks.push('unsent goal/criteria survive parent remount; save and redraw immediately (#874)');
   // Existing launch action performs a real task against the synthetic provider.
   await page.locator(`[data-plan-id="${item.id}"] .plan-launch`).click();
   for (let tries = 0; tries < 100; tries++) { s = await call('GET', `/sessions/project-records-${n}`); item = s.plan.find(p => p.id === item.id); if (item.runId && s.runs.find(r => r.id === item.runId)?.status === 'completed') break; await delay(100); }
   assert.equal(s.runs.find(r => r.id === item.runId)?.status, 'completed'); assert.equal(item.status, 'awaiting_acceptance'); row.checks.push('real execution completes while unmet goal awaits acceptance');
   await board(); let goal = page.locator(`[data-goal-item="${item.id}"]`); await goal.locator('> summary').click();
   for (let c = 0; c < 2; c++) {
    const criterion = goal.locator('.project-criterion').nth(c); await criterion.waitFor();
    await criterion.locator('select').first().selectOption('passed');
    await criterion.locator('textarea[name="note"]').fill(`Inspected actual fixture output, criterion ${c}`);
    const response = page.waitForResponse(r => r.url().includes('/acceptance') && r.request().method() === 'POST');
    await criterion.locator('button[type="submit"]').click();
    const acceptedResponse = await response; assert.equal(acceptedResponse.status(), 200, await acceptedResponse.text());
    await page.waitForFunction(({ id, c }) => document.querySelector(`[data-goal-item="${id}"]`)?.querySelectorAll('.project-criterion')[c]?.textContent.includes('Operator observation') || document.querySelector(`[data-goal-item="${id}"]`)?.querySelectorAll('.project-criterion')[c]?.textContent.includes('用户观察'), { id: item.id, c });
   }
   s = await call('GET', `/sessions/project-records-${n}`); assert.equal(s.plan.find(p => p.id === item.id).status, 'completed'); row.checks.push('each operator observation persists and all accepted criteria complete the goal');
   await page.screenshot({ path: path.join(out, `${row.id}-goal.png`) });
   if (n === 0) {
    await goal.locator('.project-contract-edit > summary').click(); const edit = goal.locator('.project-contract-edit form');
    await edit.locator('[name="criteria"]').fill('Revised requirement inspected');
    let response = page.waitForResponse(r => r.url().includes('/acceptance') && r.request().method() === 'PUT'); await edit.locator('button[type="submit"]').click(); assert.equal((await response).status(), 200);
    await page.waitForFunction(id => document.querySelector(`[data-goal-item="${id}"]`)?.querySelectorAll('.project-criterion').length === 1, item.id);
    s = await call('GET', `/sessions/project-records-${n}`); assert.equal(s.plan.find(p => p.id === item.id).status, 'awaiting_acceptance'); assert.equal(s.plan.find(p => p.id === item.id).contractHistory.length, 1);
    for (const state of ['blocked', 'passed']) {
     const criterion = goal.locator('.project-criterion'); await criterion.locator('select').first().selectOption(state); await criterion.locator('[name="note"]').fill(state === 'blocked' ? 'Needs additional review' : 'Inspected revised requirement');
     response = page.waitForResponse(r => r.url().includes('/acceptance') && r.request().method() === 'POST'); await criterion.locator('button[type="submit"]').click(); const result = await (await response).json(); assert.equal(result.accepted, state === 'passed');
     await criterion.locator('.project-source').filter({ hasText: state === 'blocked' ? 'Needs additional review' : 'Inspected revised requirement' }).first().waitFor();
    }
    await goal.getByText('历史复核记录', { exact: true }).click(); await goal.getByText(/Needs additional review/).first().waitFor();
    row.checks.push('revised conditions reset acceptance; blocked/pass reviews and prior contract are retained');
   }
   let knowledge = page.locator('.project-knowledge'); await knowledge.locator('> summary').click();
   let form = knowledge.locator('.project-knowledge-add'); await form.locator('select').selectOption('decision');
   await form.locator('[name="subject"]').fill(`Storage ${n} ${marker}`); await form.locator('[name="content"]').fill(`Use storage A ${n} ${marker}`); await form.locator('[name="source"]').fill('Synthetic design review v1'); await form.locator('button[type="submit"]').click();
   let record = knowledge.locator('.project-knowledge-record').filter({ has: page.locator('strong', { hasText: `Storage ${n} ${marker}` }) }); await record.waitFor();
   await record.getByRole('button', { name: language === 'en' ? 'Confirm this record' : '确认此记录', exact: true }).click();
   await page.waitForFunction(({ n, marker }) => Array.from(document.querySelectorAll('.project-knowledge-record')).some(r => r.textContent.includes(`Use storage A ${n} ${marker}`) && !r.textContent.includes('Confirm this record') && !r.textContent.includes('确认此记录')), { n, marker });
   let persisted = await call('GET', '/project-knowledge'); assert.ok(persisted.records.some(k => k.subject === `Storage ${n} ${marker}` && k.state === 'confirmed')); row.checks.push('source-linked decision explicitly confirmed through product UI');
   await page.screenshot({ path: path.join(out, `${row.id}-knowledge.png`) });
   if (n === 0) {
    async function propose(text, source) {
     const f = knowledge.locator('.project-knowledge-add'); await f.locator('select').selectOption('decision');
     await f.locator('[name="subject"]').fill(`Storage ${n} ${marker}`); await f.locator('[name="content"]').fill(text); await f.locator('[name="source"]').fill(source);
     const response = page.waitForResponse(r => r.url().endsWith('/api/project-knowledge') && r.request().method() === 'POST'); await f.locator('button[type="submit"]').click(); assert.equal((await response).status(), 200);
     const card = knowledge.locator('.project-knowledge-record').filter({ has: page.locator('p', { hasText: text, exact: true }) }); await card.waitFor(); return card;
    }
    const b = await propose('Second explicit decision', 'Review v2');
    let response = page.waitForResponse(r => r.url().endsWith('/api/project-knowledge') && r.request().method() === 'POST'); await b.getByRole('button', { name: '确认此记录', exact: true }).click(); let result = await (await response).json(); assert.equal(Object.keys(result.conflicts).length, 2);
    await knowledge.locator('.project-error').filter({ hasText: '同主题存在多条已确认记录' }).first().waitFor();
    const c = await propose('Final explicit decision', 'Review v3');
    const checkboxes = c.locator('input[type="checkbox"]'); assert.equal(await checkboxes.count(), 2); for (let i = 0; i < 2; i++) await checkboxes.nth(i).check();
    response = page.waitForResponse(r => r.url().endsWith('/api/project-knowledge') && r.request().method() === 'POST'); await c.getByRole('button', { name: '确认此记录', exact: true }).click(); result = await (await response).json(); assert.equal(Object.keys(result.conflicts).length, 0); assert.equal(result.records.filter(r => r.subject === `Storage ${n} ${marker}` && r.state === 'superseded').length, 2);
    const downloadEvent = page.waitForEvent('download'); await knowledge.getByRole('button', { name: '导出知识记录', exact: true }).click(); const downloaded = JSON.parse(fs.readFileSync(await (await downloadEvent).path(), 'utf8')); assert.equal(downloaded.schema, 'aide.project-knowledge/v1'); assert.ok(downloaded.records.some(r => r.content === 'Final explicit decision' && r.state === 'confirmed'));
    row.checks.push('conflict visible; explicit replacement preserves old records; actual JSON download contains current decision');
    persisted = await call('GET', '/project-knowledge');
   }
   // A stale client must retain its draft and receive conflict, not overwrite.
   form = knowledge.locator('.project-knowledge-add'); await form.locator('[name="subject"]').fill('Unsaved draft'); await form.locator('[name="content"]').fill('Draft retained after conflict'); await form.locator('[name="source"]').fill('User draft');
   await call('POST', '/project-knowledge', { action: 'propose', workspaceId: persisted.workspaceId, snapshot: persisted.snapshot, kind: 'question', subject: `External update ${n}`, content: 'Synthetic concurrent update', source: 'Second client fixture' });
   await form.locator('button[type="submit"]').click(); await form.locator('[role="alert"]').filter({ hasText: /已变化/ }).waitFor(); assert.equal(await form.locator('[name="subject"]').inputValue(), 'Unsaved draft'); row.checks.push('concurrent update rejected; unsaved draft retained');
   await page.screenshot({ path: path.join(out, `${row.id}.png`), fullPage: false });
   if (n === 0) {
    await knowledge.getByRole('button', { name: '刷新', exact: true }).click();
    await form.locator('[name="subject"]').waitFor(); assert.equal(await form.locator('[name="subject"]').inputValue(), 'Unsaved draft');
    const external = knowledge.locator('.project-knowledge-record').filter({ has: page.locator('strong', { hasText: `External update ${n}`, exact: true }) }); await external.waitFor();
    const response = page.waitForResponse(r => r.url().endsWith('/api/project-knowledge') && r.request().method() === 'POST'); await external.getByRole('button', { name: '归档此记录', exact: true }).click(); const result = await (await response).json(); assert.ok(result.records.some(r => r.subject === `External update ${n}` && r.state === 'archived')); assert.equal(await form.locator('[name="subject"]').inputValue(), 'Unsaved draft');
    row.checks.push('refresh and unrelated record archival preserve an unsent draft');
   }
   const bounds = await page.locator('.project-knowledge').boundingBox(); assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width + 2, JSON.stringify(bounds));
   // Reload and reselect another real persisted session: project records survive.
   await page.reload({ waitUntil: 'networkidle' }); if (width === 390) await page.locator('#sidebar-toggle').click();
   await page.locator(`[data-session-id="project-records-${n + 4}"]`).click(); if (width === 390 && await page.locator('#sidebar-scrim').isVisible()) await page.locator('#sidebar-scrim').click({ position: { x: width - 8, y: 80 } });
   await board(); await page.locator('.project-knowledge > summary').click(); await page.locator('.project-knowledge-record').filter({ hasText: n === 0 ? 'Final explicit decision' : `Use storage A ${n} ${marker}` }).waitFor(); row.checks.push('decision survives page reload and appears in a different session; viewport fits');
   assert.deepEqual(errors, []); row.status = 'pass';
  } catch (e) { row.error = e.stack; await page.screenshot({ path: path.join(out, `${row.id}-failed.png`) }).catch(() => {}); throw e; }
  finally { await ctx.close(); fs.writeFileSync(path.join(out, 'project-records-report.json'), JSON.stringify(report, null, 2)); }
 }
 report.status = 'pass'; fs.writeFileSync(path.join(out, 'project-records-report.json'), JSON.stringify(report, null, 2)); console.log(JSON.stringify(report));
 } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
