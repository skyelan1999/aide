'use strict';

// Read-only audit of the checked-out aide source. No browser, network, API,
// Docker, model, credential, or project-file writes are used.
// PASS means the named defect was reproduced, NOT that the product is correct.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

const project = process.argv[2] || '/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide';
const appPath = path.join(project, 'internal/server/web/app.js');
const workflowPath = path.join(project, 'internal/server/workflow.go');
const appSource = fs.readFileSync(appPath, 'utf8');
const workflowSource = fs.readFileSync(workflowPath, 'utf8');

function extract(source, pattern, description) {
  const match = source.match(pattern);
  assert.ok(match, `Source extraction failed: ${description}`);
  return match;
}
function location(source, text) {
  const index = source.indexOf(text);
  assert.ok(index >= 0);
  return source.slice(0, index).split('\n').length;
}
function confirmed(id, evidence) {
  console.log(JSON.stringify({ status: 'PASS', meaning: 'defect confirmed', id, ...evidence }));
}

(async () => {
  console.log(JSON.stringify({ audit: 'aide isolated frontend defect reproduction', timeUTC: new Date().toISOString(), project, head: execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: project, encoding: 'utf8' }).trim(), meaningOfPASS: 'The expected defect was confirmed; PASS is not a product acceptance result.' }));

  // 1. Execute the real selectSession() against controlled, reordered API replies.
  const selectCode = extract(appSource, /async function selectSession\(id\) \{[\s\S]*?\n\}/, 'selectSession')[0];
  const pending = new Map();
  const state = { poll: null, session: null };
  const context = {
    state,
    clearTimeout() {},
    api: url => new Promise(resolve => pending.set(url, resolve)),
    renderSession() {}, refreshCompactInfo() {},
    loadSessions: async () => {}, schedulePoll() {}
  };
  vm.createContext(context);
  vm.runInContext(selectCode, context);
  const requestA = context.selectSession('A');
  const requestB = context.selectSession('B');
  pending.get('/sessions/B')({ id: 'B' });
  await requestB;
  assert.equal(state.session.id, 'B');
  pending.get('/sessions/A')({ id: 'A' });
  await requestA;
  assert.equal(state.session.id, 'A', 'Expected stale A response to overwrite latest selection B');
  confirmed('late_session_response_overwrites_selection', { source: appPath, line: location(appSource, selectCode), requestOrder: ['A', 'B'], responseOrder: ['B', 'A'], lastUserSelection: 'B', actualFinalSelection: state.session.id });

  // 2. Execute the actual price getter expressions with valid zero-valued settings.
  const priceCode = extract(appSource, /const priceIn = [^\n]+\n  const priceOut = [^\n]+/, 'priceIn and priceOut')[0];
  const priceContext = { window: { aideUI: { get: () => 0 } } };
  vm.createContext(priceContext);
  const actualPrices = vm.runInContext(priceCode + '\n({input:priceIn(),output:priceOut()})', priceContext);
  assert.equal(actualPrices.input, 2);
  assert.equal(actualPrices.output, 8);
  confirmed('zero_price_replaced_by_defaults', { source: appPath, line: location(appSource, priceCode), configured: { input: 0, output: 0 }, actual: actualPrices });

  // 3. Execute the actual estimateContext() on a Chinese message, then compare
  // its included-history result with the checked-in Go replay guard.
  // Go len(string) equals UTF-8 byte length; Buffer.byteLength reproduces only
  // that stable primitive here. The backend function/service is not executed.
  const estimateCode = extract(appSource, /function estimateContext\(\) \{[\s\S]*?\n\}/, 'estimateContext')[0];
  const fmtCode = extract(appSource, /function fmtTokens\(n\) \{[^\n]+/, 'fmtTokens')[0];
  const backendGuard = extract(workflowSource, /for start > 0 && total\+len\(s\.Messages\[start-1\]\.Content\) < (\d+) \{/, 'Go history replay byte guard');
  const chineseMessage = '中'.repeat(25000);
  const elements = new Map();
  const getElement = id => {
    if (!elements.has(id)) elements.set(id, { textContent: '', title: '', style: {}, classList: { toggle() {} } });
    return elements.get(id);
  };
  const estimateContext = {
    state: { session: { messages: [{ role: 'user', content: chineseMessage }] } },
    activeModel: () => ({ contextWindow: 65536 }),
    $: getElement
  };
  vm.createContext(estimateContext);
  vm.runInContext(fmtCode + '\n' + estimateCode + '\nestimateContext();', estimateContext);
  const browserIncludes = getElement('context-card').title.startsWith('最近 1 条消息');
  const utf8Bytes = Buffer.byteLength(chineseMessage, 'utf8');
  const backendBudget = Number(backendGuard[1]);
  const backendIncludes = utf8Bytes < backendBudget;
  assert.equal(browserIncludes, true);
  assert.equal(backendIncludes, false);
  confirmed('history_replay_budget_unit_mismatch', { frontendSource: appPath, frontendLine: location(appSource, estimateCode), backendSource: workflowPath, backendLine: location(workflowSource, backendGuard[0]), browserStringLength: chineseMessage.length, utf8Bytes, backendBudget, browserIncludes, backendIncludes, browserMeter: getElement('context-stat').textContent, backendCheckMethod: 'Source guard extracted; Go len(string) compared using Node UTF-8 byte length, not a live backend call.' });

  console.log(JSON.stringify({ summary: '3/3 expected defects confirmed', projectFilesModified: false, liveServicesAccessed: false }));
})().catch(error => {
  console.error(JSON.stringify({ status: 'FAIL', meaning: 'Audit did not reproduce its expected defect or source extraction failed', error: error.message }));
  process.exitCode = 1;
});
