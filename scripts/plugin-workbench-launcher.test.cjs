'use strict';
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
class Element {
  constructor(tag, cls = '', text = '') {
    this.tag = tag; this.className = cls; this.textContent = text; this.children = [];
    this.attributes = {}; this.dataset = {}; this.listeners = {}; this.disabled = false; this.open = false;
    this.hidden = false; this.style = {}; this.type = '';
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
  const ids = Object.fromEntries(['plugin-workbench-entry', 'plugin-workbench-panel', 'plugin-workbench-grid', 'plugin-workbench-letters', 'plugin-workbench-preview', 'plugin-workbench-close'].map(id => [id, new Element(id === 'plugin-workbench-panel' ? 'dialog' : 'div')]));
  const window = { innerWidth: 1280 };
  const timers = new Map(); let timerId = 0;
  const context = { window, document: { getElementById: id => ids[id] }, console, setTimeout(fn) { timers.set(++timerId, fn); return timerId; }, clearTimeout(id) { timers.delete(id); } };
  vm.runInNewContext(fs.readFileSync('internal/server/web/plugin-workbench-launcher.js', 'utf8'), context);
  const launcher = window.AidePluginWorkbenchLauncher;
  const flush = async () => { const pending = [...timers.values()]; timers.clear(); for (const fn of pending) await fn(); };
  assert.deepEqual(Array.from(launcher.supported([{ id: 'a' }, { id: 'b', workbench: { protocol: 0 } }, { id: 'c', workbench: { protocol: 1 } }]).map(p => p.id)), ['c']);
  assert.equal(launcher.letterOf({ id: 'terminal', name: '终端' }), 'T');
  assert.equal(launcher.letterOf({ id: 'alpha', name: 'Alpha' }), 'A');
  assert.equal(launcher.letterOf({ id: '42-plugin', name: '42' }), '#');
  const plugins = [
    { id: 'normal', name: '<img src=x>', enabled: true, version: '1.2.3', workbench: { protocol: 1, title: 'Normal' } },
    { id: 'pp-labs', name: 'PjPlan', enabled: true, workbench: { protocol: 1, entry: 'standalone.html' } },
    { id: 'disabled', name: 'Disabled', enabled: false, workbench: { protocol: 1 } },
    { id: 'broken', name: 'Broken', enabled: true, error: 'load failed', workbench: { protocol: 1 } },
    { id: 'not-workbench', name: 'Other', enabled: true },
  ];
  const opened = [], guides = [], preloaded = [];
  launcher.mount({
    t: value => value,
    el: (...args) => new Element(...args),
    plugins: () => plugins,
    displayName: value => value,
    fileFirst: id => id === 'pp-labs',
    openWorkbench: async plugin => opened.push(plugin.id),
    openGuide: async plugin => guides.push(plugin.id),
    preloadWorkbench: async plugin => { preloaded.push(plugin.id); return true; },
    blocked: () => false,
    toast: () => {},
  });
  ids['plugin-workbench-entry'].onclick();
  assert.equal(ids['plugin-workbench-panel'].open, true);
  const items = ids['plugin-workbench-grid'].children;
  assert.equal(items.length, 4);
  assert.deepEqual(items.map(item => item.dataset.pluginId), ['normal', 'pp-labs', 'disabled', 'broken']);
  assert.equal(ids['plugin-workbench-panel'].dataset.order, 'default');
  assert.equal(items[0].children[0].textContent, '<');
  assert.equal(items[0].children[1].textContent, '<img src=x>');
  assert.equal(items[2].disabled, true);
  assert.equal(items[3].disabled, true);

  const rail = ids['plugin-workbench-letters'];
  assert.equal(rail.children.length, 27);
  assert.equal(rail.children.filter(button => !button.disabled).length, 4);
  rail.children.find(button => button.dataset.letter === 'P').onclick();
  assert.equal(ids['plugin-workbench-panel'].dataset.order, 'alphabetical');
  assert.deepEqual(ids['plugin-workbench-grid'].children.filter(item => item.dataset.pluginId).map(item => item.dataset.pluginId), ['broken', 'disabled', 'normal', 'pp-labs']);
  assert.deepEqual(ids['plugin-workbench-grid'].children.filter(item => item.dataset.letterHeading).map(item => item.dataset.letterHeading), ['B', 'D', 'N', 'P']);
  assert.equal(rail.children.find(button => button.dataset.letter === 'P').attributes['aria-pressed'], 'true');
  rail.children.find(button => button.dataset.letter === 'P').onclick();
  assert.equal(ids['plugin-workbench-panel'].dataset.order, 'default');
  assert.deepEqual(ids['plugin-workbench-grid'].children.filter(item => item.dataset.pluginId).map(item => item.dataset.pluginId), ['normal', 'pp-labs', 'disabled', 'broken']);

  const normal = ids['plugin-workbench-grid'].children[0];
  normal.onmouseenter(); await flush();
  assert.deepEqual(preloaded, ['normal']);
  assert.equal(ids['plugin-workbench-preview'].hidden, false);
  assert.equal(ids['plugin-workbench-preview'].dataset.preloaded, 'normal');
  assert.equal(ids['plugin-workbench-preview'].children.at(-1).textContent, '已启动后台预加载');
  normal.onmouseleave();
  assert.equal(ids['plugin-workbench-preview'].hidden, true);
  const fileFirst = ids['plugin-workbench-grid'].children[1];
  fileFirst.onmouseenter(); await flush();
  assert.deepEqual(preloaded, ['normal']);
  fileFirst.onmouseleave();

  await normal.onclick();
  await fileFirst.onclick();
  assert.deepEqual(opened, ['normal']);
  assert.deepEqual(guides, ['pp-labs']);
  assert.equal(ids['plugin-workbench-panel'].open, false);
  ids['plugin-workbench-entry'].onclick();
  ids['plugin-workbench-close'].onclick();
  assert.equal(ids['plugin-workbench-panel'].open, false);
  const html = fs.readFileSync('internal/server/web/index.html', 'utf8');
  const entry = html.match(/<button id="plugin-workbench-entry"[\s\S]*?<\/button>/)?.[0] || '';
  assert.ok(entry && !entry.includes('<span'));
  assert.match(entry, /aria-label="工作台面板"/);
  assert.match(html, /id="plugin-workbench-letters"/);
  assert.match(html, /id="plugin-workbench-preview"/);
  assert.ok(html.indexOf('/plugin-workbench-launcher.js') < html.indexOf('/app.js'));
  console.log('PASS launcher icon, default/A–Z order, safe icons, hover preview/preload and opening states');
})().catch(error => { console.error(error); process.exitCode = 1; });
