'use strict';
const assert = require('node:assert/strict');
const bridge = require('./computer-bridge.js');
const plugin = require('../plugins/computer-control/index.js');

assert.equal(bridge.appAllowed('Safari', ['Safari']), true);
assert.equal(bridge.appAllowed('Safari', ['Finder']), false);
assert.equal(bridge.pointAllowed(10, 20, 1920, 1080), true);
assert.equal(bridge.pointAllowed(-1, 20, 1920, 1080), false);
assert.equal(bridge.pointAllowed(1920, 20, 1920, 1080), false);
assert.equal(bridge.keyAllowed('ENTER'), true);
assert.equal(bridge.keyAllowed('SUPER+Q'), false);
assert.deepEqual(plugin._test.configuredApps({ allowedApps: ['Safari', ' Finder ', 'Safari'] }), ['Safari', 'Finder']);

const tools = new Map();
plugin.apply({ settings: {}, tool: spec => tools.set(spec.name, spec) });
assert.deepEqual([...tools.keys()], ['computer_status', 'computer_inspect', 'computer_snapshot', 'computer_click', 'computer_type', 'computer_key']);
async function main() {
  for (const name of ['computer_inspect', 'computer_snapshot', 'computer_click', 'computer_type', 'computer_key']) {
    await assert.rejects(Promise.resolve().then(() => tools.get(name).handler(name === 'computer_click' ? { x: 2, y: 3, approved: true } : { text: 'x', approved: true })), /允许控制的应用/);
  }
  const scoped = new Map();
  plugin.apply({ settings: { allowedApps: ['Safari'] }, tool: spec => scoped.set(spec.name, spec) });
  await assert.rejects(Promise.resolve().then(() => scoped.get('computer_click').handler({ x: 2, y: 3 })), /Aide 确认/);
  await assert.rejects(Promise.resolve().then(() => scoped.get('computer_click').handler({ x: 2, y: 3, approved: true })), /桥接令牌未配置/);
  await assert.rejects(Promise.resolve().then(() => scoped.get('computer_type').handler({ text: 'x'.repeat(2001), approved: true })), /2000 字符/);
  await assert.rejects(Promise.resolve().then(() => scoped.get('computer_key').handler({ key: 'SUPER+Q', approved: true })), /不支持该按键/);
  console.log('PASS computer-control allowlist, scoped operations, approval gate, key/text limits');
}
main().catch(error => { console.error(error); process.exitCode = 1; });
