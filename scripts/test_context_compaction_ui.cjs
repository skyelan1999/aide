'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const style = fs.readFileSync(path.join(__dirname, '../internal/server/web/style.css'), 'utf8');
const workbench = fs.readFileSync(path.join(__dirname, '../internal/server/web/workbench.css'), 'utf8');
const begin = source.indexOf('const contextCompactionTimers = new Map();');
const end = source.indexOf('async function refreshContextMeter()', begin);
assert.ok(begin >= 0 && end > begin, 'context compaction UI functions must exist');

function element(id = '') {
  const classes = new Set();
  const children = [];
  return {
    id, classes, children, dataset: {},
    classList: {
      add(name) { classes.add(name); },
      remove(name) { classes.delete(name); },
      toggle(name, on) { if (on) classes.add(name); else classes.delete(name); },
      contains(name) { return classes.has(name); },
    },
    querySelectorAll(selector) {
      assert.equal(selector, '.context-compaction-status');
      return children.filter(child => child.statusRow);
    },
    append(child) {
      if (child.statusRow) child.remove = () => { const index = children.indexOf(child); if (index >= 0) children.splice(index, 1); };
      children.push(child);
    },
    setAttribute(name, value) { (this.attributes ||= {})[name] = value; },
  };
}

const card = element('context-card');
const detail = element('context-card-detail');
const inlineState = element('context-request-state');
const sessionA = 'session-a';
const sessionB = 'session-b';
const sessionC = 'session-c';
const sessionItems = [sessionA, sessionB, sessionC].map(id => {
  const item = element(id);
  item.dataset = { sessionId: id };
  return item;
});
const [sessionItemA, sessionItemB, sessionItemC] = sessionItems;
const context = {
  state: { session: { id: sessionA }, contextCompactions: new Map() },
  document: {
    querySelectorAll(selector) {
      assert.equal(selector, '.session-item[data-session-id], .assistant-entry[data-session-id]');
      return sessionItems;
    },
  },
  $: id => ({ 'context-card': card, 'context-card-detail': detail, 'context-request-state': inlineState })[id],
  api: async () => ({ active: false }),
  setInterval: () => 1,
  clearInterval() {},
  clearTimeout() {},
  setTimeout: () => 1,
  Date,
  fmtTokens: value => `${Number(value).toLocaleString()} tokens`,
  t: (key, ...args) => key.replace(/\{(\d+)\}/g, (_, index) => args[Number(index)] ?? ''),
  el: (tag, className = '', text = '') => ({
    tag, className, textContent: text, style:{}, statusRow: className.startsWith('context-compaction-status'),
    setAttribute() {}, append(child) { (this.children ||= []).push(child); },
  }),
  renderContextMeter() {},
  latestRequestContext: () => null,
  paintContextRequestState() { inlineState.textContent = ''; inlineState.title = ''; },
  paintContextCompactionRings() {},
};
vm.createContext(context);
vm.runInContext(source.slice(begin, end), context);

for (const phase of ['completed', 'failed', 'cancelled']) {
  assert.equal(context.isTerminalContextCompaction(phase), true, `${phase} is a terminal compaction state`);
}
assert.equal(context.isTerminalContextCompaction('folding'), false, 'active compaction remains non-terminal');
assert.ok(/if\s*\(isTerminalContextCompaction\(d\.phase\)\)\s*refreshSessionSoon\(\);/.test(source), 'live terminal events refresh the saved session and context meter');

context.showContextCompaction({ sessionId: sessionB, phase: 'started', folded: 8, beforeTokens: 13600 });
assert.equal(context.state.contextCompactions.get(sessionB).phase, 'started', 'background progress is retained by session');
assert.equal(sessionItemB.classList.contains('compacting'), true, 'the compressing session uses the shared row spinner');
assert.equal(sessionItemA.classList.contains('compacting'), false, 'another session row does not spin');
assert.equal(card.classList.contains('is-compacting'), false, 'another session must not animate the current card');
assert.equal(detail.children.length, 0, 'another session must not render status in the current card');

context.state.session = { id: sessionB };
context.renderContextCompactionState(detail);
assert.equal(card.classList.contains('is-compacting'), true, 'switching back restores the active animation');
assert.equal(detail.children.length, 1, 'active compaction exposes the current stage in details');
assert.equal(inlineState.textContent, '压缩中', 'the compact inline state still identifies active compaction');

context.showContextCompaction({ sessionId: sessionB, phase: 'started', folded: 8, beforeTokens: 13600, batchStage: 'map', batch: 2, batches: 5, startedAt: Date.now() - 65000 });
context.renderContextCompactionState(detail);
assert.match(detail.children.at(-1).children[1].textContent,/批次 2\/5/);
context.showContextCompaction({ sessionId: sessionB, phase: 'started', folded: 8, beforeTokens: 13600, batchStage: 'map', batch: 2, batches: 5, retryAttempt: 2, retryDelayMs: 800 });
context.renderContextCompactionState(detail);
assert.match(detail.children.at(-1).children[1].textContent,/第 2 次重试/);
assert.doesNotMatch(source, /摘要处理中/, 'the removed processing label cannot reappear');

context.state.session = { id: sessionA };
context.renderContextCompactionState(detail);
assert.equal(card.classList.contains('is-compacting'), false, 'switching away clears the other session animation');
assert.equal(detail.querySelectorAll('.context-compaction-status').length, 0, 'status does not leak between sessions');

const staleUpdatedAt = Date.now() - 181000;
context.showContextCompaction({
  sessionId: sessionC, phase: 'folding', folded: 8, beforeTokens: 13600,
  startedAt: Date.now() - 240000, updatedAt: staleUpdatedAt,
});
// Repeated server polls must retain the server timestamp instead of making stale work look fresh.
context.showContextCompaction({
  sessionId: sessionC, phase: 'folding', folded: 8, beforeTokens: 13600,
  startedAt: Date.now() - 240000, updatedAt: staleUpdatedAt,
});
assert.equal(context.state.contextCompactions.get(sessionC).updatedAt, staleUpdatedAt, 'polling preserves the last server progress time');
context.state.session = { id: sessionC };
context.renderContextCompactionState(detail);
assert.equal(card.classList.contains('is-compacting'), false, 'stale progress stops the active animation');
assert.equal(sessionItemC.classList.contains('compacting'), false, 'stale progress stops the session row spinner');
assert.equal(sessionItemC.classList.contains('compaction-stalled'), true, 'stale state remains attached to its own session');
assert.equal(card.classList.contains('is-compaction-stalled'), true, 'stale progress gets a distinct card state');
assert.equal(detail.children.at(-1).className.includes('stalled'), true, 'stale progress gets a warning status row');
assert.equal(detail.children.at(-1).children[1].textContent.includes('最近 3 分钟无更新'), true, 'stale status shows its quiet interval');
assert.equal(detail.children.at(-1).children.find(child=>child.className==='context-compaction-elapsed').textContent.includes('已用时 4 分 0 秒'), true, 'elapsed time continues while stale');
assert.equal(vm.runInContext(`contextCompactionElapsedTimers.has('${sessionC}')`, context), true, 'elapsed timer remains active while stalled');
context.showContextCompaction({ sessionId: sessionC, phase: 'cancelled', text: '压缩已取消，原会话内容已保留' });

context.showContextCompaction({ sessionId: sessionB, phase: 'failed', preflight: true, text: '摘要请求连续失败（HTTP 503），原会话内容已保留' });
context.state.session = { id: sessionB };
context.renderContextCompactionState(detail);
assert.equal(detail.children.at(-1).children[1].textContent.includes('HTTP 503'), true, 'preflight terminal state retains the safe failure category');

context.showContextCompaction({ sessionId: sessionB, phase: 'completed', folded: 8, beforeTokens: 13600, afterTokens: 5400 });
context.state.session = { id: sessionB };
context.renderContextCompactionState(detail);
assert.equal(sessionItemB.classList.contains('compacting'), false, 'completion stops the session row spinner');
assert.equal(detail.children.at(-1).children[2].textContent.includes('13,600 → 5,400 tokens'), true, 'completion readback retains the correct session totals');
assert.equal(card.classList.contains('was-compacted'), true, 'completed animation is visible when returning');
context.showContextCompaction({ sessionId: sessionB, phase: 'completed', folded: 0, text: '历史未超阈值，无需压缩' });
context.renderContextCompactionState(detail);
assert.equal(inlineState.textContent, '无需压缩', 'no-op compaction stays inline without a popup');
assert.match(card.dataset.compactionDetail, /无需压缩/);

context.showContextCompaction({ sessionId: sessionB, phase: 'cancelled', text: '压缩已取消，原会话内容已保留' });
context.renderContextCompactionState(detail);
assert.equal(detail.children.at(-1).className.includes('cancelled'), true, 'cancelled compaction receives a terminal status row');
assert.equal(detail.children.at(-1).children[1].textContent.includes('原会话内容已保留'), true, 'cancelled status explains that history was preserved');
assert.equal(card.classList.contains('is-compacting'), false, 'cancelled progress stops its animation');
assert.equal(sessionItemC.classList.contains('compaction-stalled'), false, 'cancellation clears the stalled session row state');

const pinned = element('context-compaction-slot');
context.$ = id => ({'context-card':card,'context-card-detail':detail,'context-compaction-slot':pinned})[id];
context.showContextCompaction({sessionId:sessionB,phase:'started',folded:4,beforeTokens:9000});
assert.equal(pinned.children.length,1,'running compaction exposes a single scoped progress row');
context.showContextCompaction({sessionId:sessionB,phase:'completed',folded:4,beforeTokens:9000,afterTokens:3000});
assert.equal(pinned.children.length,1,'completion retains its concise terminal feedback');
assert.equal(pinned.children.at(-1).children.some(child=>child.className==='context-compaction-meter'),false,'legacy horizontal compaction meter is removed');
assert.match(style,/\.session-activity-badge/,'session rows have an independent compaction activity badge');
assert.match(workbench,/#context-toggle:focus-visible/,'the keyboard detail action has a focus indicator');
assert.match(workbench,/context-lane-compaction/,'each assignment keeps its own visible compaction status');
assert.doesNotMatch(style,/\.session-item\.compacting::after/,'compaction no longer reuses the session row spinner');
assert.doesNotMatch(style,/context-fold-sweep|context-compression-fold|context-card-breathe/,'legacy strip and card animations are removed');
assert.doesNotMatch(workbench,/context-compaction-fill|context-usage-spin/,'legacy compaction meter and global ring spin are removed');
const compactHandler = source.slice(source.indexOf("$('compact-button').onclick"), source.indexOf("$('context-compaction-retry').onclick"));
assert.doesNotMatch(compactHandler,/toast\(t\("已压缩|toast\(t\("历史未超阈值|throw error/,'manual compaction never creates global result or error toasts');

console.log('PASS: isolated compaction, batches, retries, elapsed time, no-op and terminal states');
