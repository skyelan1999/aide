'use strict';
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
class Element {
  constructor(tag, cls = '', text = '') {
    this.tag = tag; this.className = cls; this.textContent = text; this.children = [];
    this.attributes = {}; this.dataset = {}; this.listeners = {}; this.disabled = false; this.open = false;
    this.classList = { add: (...names) => { this.className += ' ' + names.join(' '); } };
  }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = []; this.append(...nodes); }
  setAttribute(key, value) { this.attributes[key] = value; }
  addEventListener(key, fn) { this.listeners[key] = fn; }
  showModal() { this.open = true; }
  close() { this.open = false; }
}
(async () => {
  const ids = Object.fromEntries(['plugin-workbench-entry', 'plugin-workbench-panel', 'plugin-workbench-grid', 'plugin-workbench-close'].map(id => [id, new Element(id === 'plugin-workbench-panel' ? 'dialog' : 'div')]));
  const window = {};
  const context = { window, document: { getElementById: id => ids[id] }, console };
  vm.runInNewContext(fs.readFileSync('internal/server/web/plugin-workbench-launcher.js', 'utf8'), context);
  const launcher = window.AidePluginWorkbenchLauncher;
  assert.deepEqual(Array.from(launcher.supported([{ id: 'a' }, { id: 'b', workbench: { protocol: 0 } }, { id: 'c', workbench: { protocol: 1 } }]).map(p => p.id)), ['c']);
  const plugins = [
    { id: 'normal', name: '<img src=x>', enabled: true, workbench: { protocol: 1, title: 'Normal' } },
    { id: 'pp-labs', name: 'PjPlan', enabled: true, workbench: { protocol: 1, entry: 'standalone.html' } },
    { id: 'disabled', name: 'Disabled', enabled: false, workbench: { protocol: 1 } },
    { id: 'broken', name: 'Broken', enabled: true, error: 'load failed', workbench: { protocol: 1 } },
    { id: 'not-workbench', name: 'Other', enabled: true },
  ];
  const opened = [], guides = [];
  launcher.mount({
    t: value => value,
    el: (...args) => new Element(...args),
    plugins: () => plugins,
    displayName: value => value,
    fileFirst: id => id === 'pp-labs',
    openWorkbench: async plugin => opened.push(plugin.id),
    openGuide: async plugin => guides.push(plugin.id),
    blocked: () => false,
    toast: () => {},
  });
  ids['plugin-workbench-entry'].onclick();
  assert.equal(ids['plugin-workbench-panel'].open, true);
  const items = ids['plugin-workbench-grid'].children;
  assert.equal(items.length, 4);
  assert.deepEqual(items.map(item => item.dataset.pluginId), ['normal', 'pp-labs', 'disabled', 'broken']);
  assert.equal(items[0].children[0].textContent, '<');
  assert.equal(items[0].children[1].textContent, '<img src=x>');
  assert.equal(items[2].disabled, true);
  assert.equal(items[3].disabled, true);
  await items[0].onclick();
  await items[1].onclick();
  assert.deepEqual(opened, ['normal']);
  assert.deepEqual(guides, ['pp-labs']);
  assert.equal(ids['plugin-workbench-panel'].open, false);
  ids['plugin-workbench-entry'].onclick();
  ids['plugin-workbench-close'].onclick();
  assert.equal(ids['plugin-workbench-panel'].open, false);
  const html = fs.readFileSync('internal/server/web/index.html', 'utf8');
  assert.match(html, /id="plugin-workbench-entry"[\s\S]*?data-i18n="工作台面板"/);
  assert.match(html, /id="plugin-workbench-panel"/);
  assert.ok(html.indexOf('/plugin-workbench-launcher.js') < html.indexOf('/app.js'));
  console.log('PASS workbench launcher capability filter, dialog, plugin icons, disabled states, normal/file-first opening and safe names');
})().catch(error => { console.error(error); process.exitCode = 1; });
