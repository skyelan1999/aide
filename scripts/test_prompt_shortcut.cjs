'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
const helper = app.match(/function shouldSendPromptOnKeydown\(event\) \{[\s\S]*?\n\}/);
assert.ok(helper, 'prompt send shortcut helper should exist');
const sandbox = {};
vm.runInNewContext(helper[0], sandbox);
const shouldSend = event => Boolean(sandbox.shouldSendPromptOnKeydown(event));

assert.equal(shouldSend({ key: 'Enter', shiftKey: true }), true, 'Shift+Enter sends');
assert.equal(shouldSend({ key: 'Enter' }), false, 'Enter remains a newline');
assert.equal(shouldSend({ key: 'Enter', ctrlKey: true }), false, 'Ctrl+Enter does not send');
assert.equal(shouldSend({ key: 'Enter', metaKey: true }), false, 'Command+Enter does not send');
assert.equal(shouldSend({ key: 'Enter', shiftKey: true, isComposing: true }), false, 'IME composition is not submitted');
assert.equal(shouldSend({ key: 'Enter', shiftKey: true, altKey: true }), false, 'Alt+Shift+Enter does not send');
console.log('prompt shortcut PASS');
