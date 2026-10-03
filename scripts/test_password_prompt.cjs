'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const start = source.indexOf('function passwordPrompt(title) {');
const end = source.indexOf('// requestMasterAuth(', start);
assert.ok(start >= 0 && end > start, 'password prompt exists');

const dialogs = [];
function el(tag, className = '', text = '') {
  return {
    tag, className, textContent: text, children: [], attributes: {}, listeners: {},
    append(...nodes) { this.children.push(...nodes); },
    setAttribute(name, value) { this.attributes[name] = value; },
    addEventListener(name, callback) { this.listeners[name] = callback; },
    focus() {}, close() {}, remove() {}, showModal() {},
  };
}
const context = {
  Promise,
  t: key => key,
  el,
  document: { body: { append(dialog) { dialogs.push(dialog); } } },
};
vm.createContext(context);
vm.runInContext(source.slice(start, end), context);

(async () => {
  const cancelled = context.passwordPrompt('Enter password');
  const dialog = dialogs.at(-1);
  const [label, actions] = dialog.children;
  const [field] = label.children;
  const [input, reveal] = field.children;
  assert.equal(input.type, 'password');
  assert.equal(input.autocomplete, 'current-password');
  assert.equal(reveal.textContent, '显示');
  reveal.onclick();
  assert.equal(input.type, 'text');
  assert.equal(reveal.textContent, '隐藏');
  assert.equal(reveal.attributes['aria-pressed'], 'true');
  reveal.onclick();
  assert.equal(input.type, 'password');
  assert.equal(reveal.attributes['aria-pressed'], 'false');
  actions.children[0].onclick();
  assert.equal(await cancelled, null);

  const accepted = context.passwordPrompt('Enter password');
  const acceptedDialog = dialogs.at(-1);
  const acceptedInput = acceptedDialog.children[0].children[0].children[0];
  acceptedInput.value = 'sample-secret';
  acceptedDialog.children[1].children[1].onclick();
  assert.equal(await accepted, 'sample-secret');
  assert.equal(acceptedInput.value, '');
  console.log('password prompt PASS');
})().catch(error => { console.error(error); process.exitCode = 1; });
