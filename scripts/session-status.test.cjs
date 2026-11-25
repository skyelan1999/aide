'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const begin = source.indexOf('function sessionStatusPresentation(');
const end = source.indexOf('let sessionListRequest', begin);
assert.ok(begin >= 0 && end > begin, 'session status presentation helper must exist');
const context = {};
vm.createContext(context);
vm.runInContext(source.slice(begin, end), context);
const present = context.sessionStatusPresentation;

const cases = [
  ['running', 'running', '运行中'],
  ['resumed', 'running', '运行中'],
  ['queued', 'queued', '排队中'],
  ['awaiting_clarification', 'awaiting-clarification', '待回答'],
  ['awaiting_approval', 'awaiting-approval', '待审批'],
  ['paused', 'paused', '已暂停'],
  ['failed', 'failed', '失败'],
  ['interrupted', 'interrupted', '已中断'],
  ['cancelled', 'cancelled', '已停止'],
];
for (const [status, key, label] of cases) {
  const value = present({ status });
  assert.equal(value.key, key, status);
  assert.equal(value.label, label, status);
  assert.equal(typeof value.icon, 'string', status);
}
assert.equal(present({ status: 'completed', checked: false }).key, 'completed-unread');
assert.equal(present({ status: 'completed', checked: false }).label, '新结果');
assert.equal(present({ status: 'completed', checked: true }), null);
assert.equal(present({ status: 'completed', checked: true }, true), null);
assert.equal(present({ status: 'completed', checked: true, autoArchived: true }, true).label, '已完成');
assert.equal(present({ status: '' }), null);
assert.equal(present({ status: 'unknown' }), null);

const style = fs.readFileSync(path.join(__dirname, '../internal/server/web/style.css'), 'utf8');
assert.match(style, /\.session-status-badge/);
assert.match(style, /\.status-awaiting-clarification/);
assert.match(style, /\.status-failed/);
assert.match(style, /\.session-activity-badge\[hidden\]\{display:none\}/);
assert.doesNotMatch(style, /\.session-item\.compacting::after/);
assert.match(source, /activity\.textContent = kind \? t\(labels\[kind\]\) : ''/);
console.log('PASS: session status labels, unread separation, and auxiliary activity styles');
