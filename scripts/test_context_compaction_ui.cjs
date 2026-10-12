'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const begin = source.indexOf('const contextCompactionTimers = new Map();');
const end = source.indexOf('async function refreshContextMeter()', begin);
assert.ok(begin >= 0 && end > begin, 'context compaction UI functions must exist');

function element(id = '') {
  const classes = new Set();
  const children = [];
  return {
    id, classes, children,
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
const sessionA = 'session-a';
const sessionB = 'session-b';
const context = {
  state: { session: { id: sessionA }, contextCompactions: new Map() },
  $: id => ({ 'context-card': card, 'context-card-detail': detail })[id],
  api: async () => ({ active: false }),
  setInterval: () => 1,
  clearInterval() {},
  clearTimeout() {},
  setTimeout: () => 1,
  Date,
  fmtTokens: value => `${Number(value).toLocaleString()} tokens`,
  t: (key, ...args) => key.replace(/\{(\d+)\}/g, (_, index) => args[Number(index)] ?? ''),
  el: (tag, className = '', text = '') => ({
    tag, className, textContent: text, statusRow: className.startsWith('context-compaction-status'),
    setAttribute() {}, append(child) { (this.children ||= []).push(child); },
  }),
  renderContextMeter() {},
};
vm.createContext(context);
vm.runInContext(source.slice(begin, end), context);

context.showContextCompaction({ sessionId: sessionB, phase: 'started', folded: 8, beforeTokens: 13600 });
assert.equal(context.state.contextCompactions.get(sessionB).phase, 'started', 'background progress is retained by session');
assert.equal(card.classList.contains('is-compacting'), false, 'another session must not animate the current card');
assert.equal(detail.children.length, 0, 'another session must not render status in the current card');

context.state.session = { id: sessionB };
context.renderContextCompactionState(detail);
assert.equal(card.classList.contains('is-compacting'), true, 'switching back restores the active animation');
assert.equal(detail.children.at(-1).children[1].textContent.includes('正在整理'), true, 'active phase is visible after returning');

context.showContextCompaction({ sessionId: sessionB, phase: 'started', folded: 8, beforeTokens: 13600, batchStage: 'map', batch: 2, batches: 5, startedAt: Date.now() - 65000 });
context.renderContextCompactionState(detail);
assert.equal(detail.children.at(-1).children[1].textContent.includes('分段整理 2/5'), true, 'automatic compaction shows map batch progress');
assert.equal(detail.children.at(-1).children.at(-1).textContent.includes('已用时 1 分 5 秒'), true, 'active compaction exposes elapsed time');

context.state.session = { id: sessionA };
context.renderContextCompactionState(detail);
assert.equal(card.classList.contains('is-compacting'), false, 'switching away clears the other session animation');
assert.equal(detail.querySelectorAll('.context-compaction-status').length, 0, 'status does not leak between sessions');

context.showContextCompaction({ sessionId: sessionB, phase: 'completed', folded: 8, beforeTokens: 13600, afterTokens: 5400 });
context.state.session = { id: sessionB };
context.renderContextCompactionState(detail);
assert.equal(detail.children.at(-1).children[2].textContent.includes('13,600 tokens → 5,400 tokens'), true, 'completion readback retains the correct session totals');
assert.equal(card.classList.contains('was-compacted'), true, 'completed animation is visible when returning');

console.log('PASS: compaction progress is retained per session and never leaks across session switches');
