'use strict';
const { chromium } = require('./browser-runtime/node_modules/playwright');
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict');
const base = process.env.AIDE_TEST_URL, token = process.env.AIDE_TEST_TOKEN, out = process.env.AIDE_EVIDENCE_DIR;
if (!base || !token || !out) throw Error('Use the isolated project records fixture and evidence directory');
const report = { status: 'fail', scope: 'Actual HTTP/DOM/persistence with synthetic project files and an unpaid deterministic provider', cases: [] };
fs.mkdirSync(out, { recursive: true });
(async () => {
 const browser = await chromium.launch({ headless: true });
 try {
 let n = 0;
 for (const language of ['zh-CN', 'en']) for (const width of [1440, 390]) {
  const index = n++, sid = `workflow-records-${index}`, row = { id: `${language}-${width}`, status: 'fail', checks: [] }; report.cases.push(row);
  const context = await browser.newContext({ viewport: { width, height: 1000 }, reducedMotion: 'reduce' });
  await context.addInitScript(({ token, language, width }) => { localStorage.setItem('aide-token', token); localStorage.setItem('aide.ui', JSON.stringify({ version: 1, language, palette: width === 390 ? 'comfort' : 'blue', theme: width === 390 ? 'dark' : 'light', motion: 'reduced', startupAnimation: 'off' })); }, { token, language, width });
  const page = await context.newPage(), errors = [], runtimeErrors = []; page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message));
  const call = async (method, endpoint, data) => { const r = await context.request.fetch(base + '/api' + endpoint, { method, headers: { Authorization: 'Bearer ' + token }, data }); assert.ok(r.ok(), `${method} ${endpoint}: ${r.status()} ${await r.text()}`); return r.json(); };
  async function openSession() {
   runtimeErrors.push(...await page.evaluate(() => window.workflowRuntimeToasts || []).catch(() => []));
   await page.goto(base, { waitUntil: 'networkidle' }); await page.evaluate(() => { window.workflowRuntimeToasts = []; const original = window.toast; window.toast = text => { if (/Cannot read properties|is not a function|is not defined/u.test(String(text))) window.workflowRuntimeToasts.push(String(text)); return original(text); }; }); if (width === 390) await page.locator('#sidebar-toggle').click();
   await page.locator(`[data-session-id="${sid}"]`).click(); if (width === 390 && await page.locator('#sidebar-scrim').isVisible()) await page.locator('#sidebar-scrim').click({ position: { x: width - 8, y: 80 } });
   await board(); assert.deepEqual(await page.evaluate(() => window.workflowRuntimeToasts), []);
  }
  async function board() { if (!await page.locator('#trajectory-sheet').evaluate(n => n.classList.contains('open'))) await page.locator('#trajectory-toggle').click(); await page.locator('.traj-main-seg [data-view="outcome"]').click(); const picker = page.locator('#traj-outcome-run'); if (await picker.count()) await picker.selectOption(''); await page.locator('.plan-add').waitFor(); }
  async function submitResponse(control, endpoint, method) { const event = page.waitForResponse(r => r.url().endsWith(endpoint) && r.request().method() === method); await control.click(); const response = await event; assert.ok(response.ok(), await response.text()); return response.json(); }
  async function executeStage(item, guided = false) {
   await board(); if (guided) await page.locator(".project-flow-guide .primary").click(); else await page.locator(`[data-plan-id="${item.id}"] .plan-launch`).click();
   for (let tries = 0; tries < 150; tries++) { const s = await call('GET', `/sessions/${sid}`), current = s.plan.find(v => v.id === item.id), run = s.runs.find(v => v.id === current.runId); if (run?.status === 'completed') return { current, run }; await page.waitForTimeout(100); }
   throw Error('Stage did not complete');
  }
  async function acceptStage(item) {
   await board(); const goal = page.locator(`[data-goal-item="${item.id}"]`); if (!await goal.evaluate(n => n.open)) await goal.locator('> summary').click();
   for (let i = 0; i < 2; i++) { const c = goal.locator('.project-criterion').nth(i); await c.locator('select').first().selectOption('passed'); await c.locator('[name="note"]').fill(`Inspected synthetic stage ${item.id} criterion ${i}`); await submitResponse(c.locator('button[type="submit"]'), `/api/sessions/${sid}/plan/${item.id}/acceptance`, 'POST'); await c.locator('.project-source').filter({ hasText: `criterion ${i}` }).first().waitFor(); }
  }
  try {
   await openSession(); row.checks.push('empty legacy session opens without a runtime-error toast (#878)'); const impact = page.locator('.project-impact'); await impact.locator('> summary').click();
   const create = impact.locator('.project-impact-add'); await create.locator('[name="change"]').fill(`Pressure change ${index}`); await create.locator('.project-seed-picker input[type="search"]').fill('spec'); await create.locator('.project-seed-picker input[value="spec.md"]').check();
   const view = await submitResponse(create.locator('button[type="submit"]'), `/api/sessions/${sid}/impacts`, 'POST'); const analysis = view.analyses[0].analysis; assert.ok(analysis.items.length >= 3, JSON.stringify(analysis));
   let saved = impact.locator(`[data-impact-id="${analysis.id}"]`); if (!await saved.evaluate(n => n.open)) await saved.locator('> summary').click(); await saved.locator('.project-criterion').filter({ hasText: 'report.md' }).waitFor(); row.checks.push('indexed specification, calculation and report relations become saved review candidates');
   const item = saved.locator('.project-impact-review').first(); await item.locator('[name="classification"]').selectOption('unaffected'); await item.locator('[name="state"]').selectOption('done'); await item.locator('[name="note"]').fill('Inspected synthetic file and checked actual applicability');
   await submitResponse(item.locator('button[type="submit"]'), `/api/sessions/${sid}/impacts`, 'POST'); saved = impact.locator(`[data-impact-id="${analysis.id}"]`); if (!await saved.evaluate(n => n.open)) await saved.locator('> summary').click(); assert.equal(await saved.locator('[name="note"]').first().inputValue(), 'Inspected synthetic file and checked actual applicability'); row.checks.push('operator classification, completion and observation persist');
   if (index === 0) {
    await saved.locator('[name="note"]').first().fill('Unsent review retained after conflict'); const external = await call('GET', `/sessions/${sid}/impacts`); await call('POST', `/sessions/${sid}/impacts`, { workspaceId: external.workspaceId, snapshot: external.snapshot, action: 'analyze', change: 'Second client update', paths: ['calc.md'] });
    const rejected = page.waitForResponse(r => r.url().endsWith(`/api/sessions/${sid}/impacts`) && r.request().method() === 'POST'); await saved.locator('button[type="submit"]').first().click(); assert.equal((await rejected).status(), 409); assert.equal(await saved.locator('[name="note"]').first().inputValue(), 'Unsent review retained after conflict');
    await impact.getByRole('button', { name: '刷新', exact: true }).click(); await impact.locator(`[data-impact-id="${analysis.id}"]`).waitFor(); saved = impact.locator(`[data-impact-id="${analysis.id}"]`); if (!await saved.evaluate(n => n.open)) await saved.locator('> summary').click(); assert.equal(await saved.locator('[name="note"]').first().inputValue(), 'Unsent review retained after conflict'); row.checks.push('stale write rejected and unsent review retained across refresh');
    const event = page.waitForEvent('download'); await impact.getByRole('button', { name: '导出影响清单', exact: true }).click(); const downloaded = JSON.parse(fs.readFileSync(await (await event).path(), 'utf8')); assert.equal(downloaded.schema, 'aide.change-impact/v1'); assert.ok(downloaded.analyses.some(v => v.analysis.id === analysis.id)); row.checks.push('actual downloaded impact JSON includes stored evidence and reviews');
   }
   await saved.locator('[name="note"]').first().scrollIntoViewIfNeeded(); await page.screenshot({ path: path.join(out, `workflow-${row.id}-impact.png`) });
   const flow = page.locator('.project-business-flow'); await flow.locator('> summary').click(); const form = flow.locator('.project-flow-add'); await form.locator('[name="recipe"]').selectOption('software'); await form.locator('[name="brief"]').fill(`Maintain synthetic fixture ${index}`);
   if (language === 'en') await flow.getByText(/Understand the problem/).waitFor();
   const created = await submitResponse(form.locator('button[type="submit"]'), `/api/sessions/${sid}/plan/flows`, 'POST'); assert.equal(created.items.length, 5); const [first, second, third] = created.items;
   await page.locator(`[data-plan-id="${first.id}"]`).waitFor(); assert.equal(await page.locator(`[data-plan-id="${second.id}"] .plan-launch`).isDisabled(), true); if (language === 'en') assert.ok(first.title.includes('Software maintenance')); row.checks.push('localized recipe materializes five accepted-goal stages with enforced dependencies');
   const firstRun = await executeStage(first, true); assert.equal(firstRun.current.status, 'awaiting_acceptance'); await acceptStage(first); await board(); await page.waitForFunction(title => document.querySelector('.project-flow-guide [aria-current=step]')?.textContent.includes(title), second.title); row.checks.push('next-step navigator starts the correct stage and advances only after acceptance');
   const secondRun = await executeStage(second); assert.equal(secondRun.run.flowInputs[0].runId, firstRun.run.id); assert.ok(secondRun.run.flowInputs[0].output.includes('PROJECT_GOAL_FIXTURE')); await acceptStage(second); row.checks.push('real synthetic runs require acceptance and carry predecessor output/run provenance into the next stage');
   await board(); const secondGoal = page.locator(`[data-goal-item="${second.id}"]`); if (!await secondGoal.evaluate(n => n.open)) await secondGoal.locator('> summary').click(); await secondGoal.getByText(language === 'en' ? 'Stage inputs and provenance' : '阶段输入与来源', { exact: true }).click(); await secondGoal.getByText(/PROJECT_GOAL_FIXTURE/).waitFor(); await secondGoal.getByText(/PROJECT_GOAL_FIXTURE/).scrollIntoViewIfNeeded(); await page.screenshot({ path: path.join(out, `workflow-${row.id}-handoff.png`) });
   const toolBox = page.locator(`[data-plan-id="${third.id}"] .project-flow-tools`); await toolBox.locator('> summary').click(); await toolBox.locator('input[value="flow_value"]').check(); await submitResponse(toolBox.locator('button'), `/api/sessions/${sid}/plan/flows`, 'POST');
   const gap = await executeStage(third); assert.equal(gap.current.status, 'blocked'); const s = await call('GET', `/sessions/${sid}`); assert.equal(s.plan.find(v => v.id === first.id).status, 'completed'); assert.equal(s.plan.find(v => v.id === second.id).status, 'completed'); row.checks.push('configured plugin receipt cannot be skipped; blocked stage preserves completed predecessors');
   await board(); const firstGoal = page.locator(`[data-goal-item="${first.id}"]`); if (!await firstGoal.evaluate(n => n.open)) await firstGoal.locator('> summary').click(); await firstGoal.locator('.project-contract-edit > summary').click(); const edit = firstGoal.locator('.project-contract-edit form'); await edit.locator('[name="criteria"]').fill('Revised source requirement'); await submitResponse(edit.locator('button[type="submit"]'), `/api/sessions/${sid}/plan/${first.id}/acceptance`, 'PUT');
   const stale = await call('GET', `/sessions/${sid}`); assert.equal(stale.runs.find(r => r.id === secondRun.run.id).goalInputsStale, true); assert.equal(stale.plan.find(v => v.id === second.id).status, 'blocked'); row.checks.push('revised predecessor invalidates downstream acceptance transitively');
   await openSession(); await page.locator('.project-impact > summary').click(); const persisted = page.locator(`[data-impact-id="${analysis.id}"]`); if (!await persisted.evaluate(n => n.open)) await persisted.locator('> summary').click(); assert.equal(await persisted.locator('[name="note"]').first().inputValue(), 'Inspected synthetic file and checked actual applicability');
   assert.equal(await page.locator('.plan-row').count(), 5); const bounds = await page.locator('.project-impact').boundingBox(); assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width + 2); assert.deepEqual(errors, []); assert.deepEqual(runtimeErrors, []); assert.deepEqual(await page.evaluate(() => window.workflowRuntimeToasts), []); row.checks.push('workflow and impact reviews survive reload; viewport fits without page errors');
   await page.screenshot({ path: path.join(out, `workflow-${row.id}.png`) }); row.status = 'pass';
  } catch (e) { row.error = e.stack; await page.screenshot({ path: path.join(out, `workflow-${row.id}-failed.png`) }).catch(() => {}); throw e; }
  finally { await context.close(); fs.writeFileSync(path.join(out, 'project-workflows-report.json'), JSON.stringify(report, null, 2)); }
 }
 report.status = 'pass'; fs.writeFileSync(path.join(out, 'project-workflows-report.json'), JSON.stringify(report, null, 2)); console.log(JSON.stringify(report));
 } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
