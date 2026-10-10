const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const start = source.indexOf('function toast(text)');
const end = source.indexOf('\n}', start) + 2;
assert(start >= 0 && end > start);
let notice = null, dialog = null, serial = 0;
const timers = new Map();
const body = {append(node) {node.parentElement = this; notice = node;}};
const createElement = () => ({classList: {add() {}, remove() {}}, setAttribute() {}});
const document = {body, querySelector: () => dialog, createElement};
const context = {document, $: () => notice,
  setTimeout: fn => {timers.set(++serial, fn); return serial;},
  clearTimeout: id => timers.delete(id)};
vm.runInNewContext(source.slice(start, end), context);
dialog = {append(node) {node.parentElement = this; notice = node;},
  addEventListener(event, fn, options) {assert.equal(event, 'close'); assert.equal(options.once, true); this.close = fn;}};
context.toast('conflict');
const original = notice;
dialog.close();
assert.equal(original.parentElement, body, 'dialog close must preserve the shared notification');
dialog = null;
context.toast('restored');
assert.equal(notice, original);
assert.equal(notice.textContent, 'restored');
assert.equal(timers.size, 1, 'only the latest notification timer remains');
notice = null;
context.toast('recovered missing element');
assert.equal(notice.id, 'toast');
assert.equal(notice.textContent, 'recovered missing element');
notice = null;
assert.doesNotThrow(() => [...timers.values()].forEach(fn => fn()), 'timer must use its captured element');
console.log('PASS: toast dialog close, reuse, recreation and captured timer');
