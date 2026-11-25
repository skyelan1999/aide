'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '..');
const source = fs.readFileSync(path.join(root, 'internal/server/web/output-rate.js'), 'utf8');
const window = {};
vm.runInNewContext(source, { window }, { filename: 'output-rate.js' });
assert.equal(typeof window.AideOutputRate.create, 'function');

let now = 0;
let scheduled = null;
let cancelled = 0;
const element = { hidden: true, textContent: '', title: '' };
const translate = (key, ...args) => key.replace(/\{(\d+)\}/g, (_, index) => String(args[Number(index)]));
const controller = window.AideOutputRate.create({
  element,
  translate,
  now: () => now,
  schedule: callback => { scheduled = callback; return 1; },
  cancel: () => { cancelled++; scheduled = null; },
});

controller.record('main', 12);
assert.deepEqual(JSON.parse(JSON.stringify(controller.snapshot())), { rate: 12, tokens: 12, active: 1 });
controller.record('parallel/part-1', 8);
let current = controller.snapshot();
assert.equal(current.rate, 20);
assert.equal(current.active, 2, 'parallel rates must be summed, not maxed');
controller.render();
assert.equal(element.hidden, false);
assert.match(element.textContent, /20\.0 tok\/s/);
assert.match(element.textContent, /2 路合计/);
assert.match(element.title, /全部模型请求/);

now = 1000;
controller.record('main', 12);
controller.record('parallel/part-1', 8);
current = controller.snapshot();
assert.equal(current.tokens, 40);
assert.equal(current.rate, 40);

now = 3100;
current = controller.snapshot();
assert.equal(current.tokens, 20, 'samples older than three seconds must expire');
assert.equal(current.active, 2);
assert.ok(Math.abs(current.rate - (20 / 2.1)) < 0.000001);

now = 4101;
controller.render();
assert.equal(element.hidden, true, 'idle rate must disappear');
assert.equal(element.textContent, '');
assert.equal(controller.snapshot().tokens, 0);

controller.record('main', 5);
controller.reset();
assert.equal(controller.snapshot().tokens, 0);
assert.equal(element.hidden, true);
assert.equal(cancelled, 1);
controller.record('main', 10);
controller.record('main', -3);
assert.equal(controller.snapshot().tokens, 7, 'provider usage corrections must adjust the current session total');
controller.reset();
for (const invalid of [0, NaN, Infinity, 1000001, -1000001]) controller.record('main', invalid);
assert.equal(controller.snapshot().tokens, 0);

const app = fs.readFileSync(path.join(root, 'internal/server/web/app.js'), 'utf8');
assert.match(app, /addEventListener\('output-rate'/);
assert.match(app, /composerOutputRate\?\.record/);
assert.match(app, /composerOutputRate\?\.reset/);
assert.match(app, /sessionsEv\.addEventListener\('output-rate'/);
assert.match(app, /d\.sessionId\s*!==\s*state\.session\?\.id/);
const closeStreamBody = app.match(/function closeStream\(\) \{[\s\S]*?\n\}/)?.[0] || '';
assert.ok(closeStreamBody, 'closeStream must exist');
assert.doesNotMatch(closeStreamBody, /composerOutputRate/, 'closing one answer stream must not erase other session requests');
const html = fs.readFileSync(path.join(root, 'internal/server/web/index.html'), 'utf8');
assert.ok(html.indexOf('/output-rate.js') < html.indexOf('/app.js'), 'rate module must load before app.js');
assert.match(html, /id="output-rate"/);
assert.match(html, /app\.js\?v=311/);
console.log('output-rate tests passed');
