'use strict';
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
class Element {
  constructor(tag, cls = '', text = '') {
    this.tag = tag; this.className = cls; this.textContent = text; this.children = [];
    this.attributes = {}; this.dataset = {}; this.listeners = {}; this.disabled = false; this.open = false;
    this.hidden = false; this.style = {}; this.type = '';
    this.replaceChildrenCalls = 0; this.scrollRequests = [];
    this.classList = { add: (...names) => { this.className += ' ' + names.join(' '); }, toggle: (name, force) => { const present = this.className.split(/\s+/).includes(name); if (force === true || (force === undefined && !present)) this.className += ' ' + name; if (force === false || (force === undefined && present)) this.className = this.className.split(/\s+/).filter(item => item && item !== name).join(' '); } };
  }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.replaceChildrenCalls++; this.children = []; this.append(...nodes); }
  setAttribute(key, value) { this.attributes[key] = value; }
  getAttribute(key) { return this.attributes[key]; }
  querySelector(selector) {
    const letter = selector.match(/^\[data-letter-heading="([A-Z#])"\]$/)?.[1];
    const search = node => {
      if (letter && node.dataset?.letterHeading === letter) return node;
      for (const child of node.children || []) { const found = search(child); if (found) return found; }
      return null;
    };
    return search(this);
  }
  scrollIntoView(options) { this.scrollRequests.push(options); }
  addEventListener(key, fn) { this.listeners[key] = fn; }
  showModal() { this.open = true; }
  close() { this.open = false; }
}
(async () => {
  const ids = Object.fromEntries(['plugin-workbench-entry', 'plugin-workbench-panel', 'plugin-workbench-grid', 'plugin-workbench-letters', 'plugin-workbench-preview', 'plugin-workbench-close', 'plugin-workbench-updates', 'plugin-workbench-update-count', 'plugin-workbench-autostart', 'plugin-autostart-dialog', 'plugin-autostart-list', 'plugin-autostart-empty', 'plugin-autostart-close'].map(id => [id, new Element(id.endsWith('panel') || id.endsWith('dialog') ? 'dialog' : 'div')]));
  let checkResult = { updates: [], failures: [] };
  const upgraded = [];
  const window = { innerWidth: 1280, AidePluginUpgrade: {
    checkAll: async () => checkResult,
    upgradeAll: async updates => { upgraded.push(...updates.map(item => item.id)); return { succeeded: updates, failures: [] }; },
  } };
  const timers = new Map(); let timerId = 0;
  const context = { window, document: { getElementById: id => ids[id] }, console, setTimeout(fn) { timers.set(++timerId, fn); return timerId; }, clearTimeout(id) { timers.delete(id); } };
  vm.runInNewContext(fs.readFileSync('internal/server/web/plugin-workbench-launcher.js', 'utf8'), context);
  const launcher = window.AidePluginWorkbenchLauncher;
  const flush = async () => { const pending = [...timers.values()]; timers.clear(); for (const fn of pending) await fn(); };
  const settle = async () => { for (let i = 0; i < 5; i++) await Promise.resolve(); };
  assert.deepEqual(Array.from(launcher.supported([{ id: 'a' }, { id: 'b', workbench: { protocol: 0 } }, { id: 'c', workbench: { protocol: 1 } }]).map(p => p.id)), ['c']);
  assert.equal(launcher.letterOf({ id: 'terminal', name: '终端' }), 'T');
  assert.equal(launcher.letterOf({ id: 'alpha', name: 'Alpha' }), 'A');
  assert.equal(launcher.letterOf({ id: '42-plugin', name: '42' }), '#');
  const plugins = [
    { id: 'normal', name: '<img src=x>', enabled: true, version: '1.2.3', daemon: true, autoStart: false, workbench: { protocol: 1, title: 'Normal' } },
    { id: 'pp-labs', name: 'PjPlan', enabled: true, daemon: true, workbench: { protocol: 1, entry: 'standalone.html' } },
    { id: 'disabled', name: 'Disabled', enabled: false, workbench: { protocol: 1 } },
    { id: 'broken', name: 'Broken', enabled: true, error: 'load failed', workbench: { protocol: 1 } },
    { id: 'not-workbench', name: 'Other', enabled: true },
  ];
  checkResult = { updates: [{ id: 'normal', name: plugins[0].name, version: '2.0.0' }], failures: [] };
  const opened = [], guides = [], preloaded = [], apiCalls = [], toasts = [];
  launcher.mount({
    api: async (path, options) => { apiCalls.push({ path, options }); return { ok: true }; },
    refreshPlugins: async () => {},
    t: value => value,
    el: (...args) => new Element(...args),
    plugins: () => plugins,
    displayName: value => value,
    fileFirst: id => id === 'pp-labs',
    openWorkbench: async plugin => opened.push(plugin.id),
    openGuide: async plugin => guides.push(plugin.id),
    preloadWorkbench: async plugin => { preloaded.push(plugin.id); return true; },
    blocked: () => false,
    toast: message => toasts.push(message),
  });
  ids['plugin-workbench-entry'].onclick(); await settle();
  assert.equal(ids['plugin-workbench-panel'].open, true);
  assert.equal(ids['plugin-workbench-update-count'].hidden, false);
  assert.equal(ids['plugin-workbench-update-count'].textContent, '1');
  const grid = ids['plugin-workbench-grid'];
  assert.equal(grid.replaceChildrenCalls, 1, 'opening and update checks must not rebuild unchanged cards');
  const items = grid.children;
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
  const groupedChildren = grid.children;
  const groupedRenderCount = grid.replaceChildrenCalls;
  rail.children.find(button => button.dataset.letter === 'B').onclick();
  assert.equal(grid.replaceChildrenCalls, groupedRenderCount, 'letter navigation must keep the existing card DOM');
  assert.equal(grid.children, groupedChildren);
  assert.equal(rail.children.find(button => button.dataset.letter === 'B').attributes['aria-pressed'], 'true');
  assert.equal(rail.children.find(button => button.dataset.letter === 'P').attributes['aria-pressed'], 'false');
  const headingB = grid.children.find(item => item.dataset.letterHeading === 'B');
  assert.equal(headingB.scrollRequests[0].block, 'start');
  assert.equal(headingB.scrollRequests[0].behavior, 'auto');
  rail.children.find(button => button.dataset.letter === 'B').onclick();
  assert.equal(ids['plugin-workbench-panel'].dataset.order, 'default');
  assert.deepEqual(ids['plugin-workbench-grid'].children.filter(item => item.dataset.pluginId).map(item => item.dataset.pluginId), ['normal', 'pp-labs', 'disabled', 'broken']);

  const normal = ids['plugin-workbench-grid'].children[0];
  const panel = ids['plugin-workbench-panel'], preview = ids['plugin-workbench-preview'];
  panel.getBoundingClientRect = () => ({ left: 100, top: 50, right: 960, bottom: 730, width: 860, height: 680 });
  grid.getBoundingClientRect = () => ({ left: 128, top: 184, right: 930, bottom: 730, width: 802, height: 546 });
  preview.getBoundingClientRect = () => ({ width: 264, height: 190 });
  normal.getBoundingClientRect = () => ({ left: 700, right: 820, top: 600, bottom: 758, width: 120, height: 158 });
  normal.onmouseenter(); await flush();
  assert.deepEqual(preloaded, []);
  assert.equal(preview.hidden, false);
  assert.equal(preview.dataset.preloaded, undefined);
  assert.equal(preview.children.at(-1).textContent, '点击打开');
  assert.equal(preview.style.left, '326px', 'right-side cards must open details toward the dialog interior');
  assert.equal(preview.style.top, '482px', 'bottom-side details must move above the clipping boundary');
  normal.onmouseleave();
  assert.equal(preview.hidden, true);
  normal.getBoundingClientRect = () => ({ left: 130, right: 250, top: 100, bottom: 258, width: 120, height: 158 });
  normal.onmouseenter(); await flush();
  assert.equal(preview.style.left, '160px', 'left-side cards should open details to the right');
  assert.equal(preview.style.top, '142px', 'details must not overlap the dialog header');
  normal.onmouseleave();
  const fileFirst = ids['plugin-workbench-grid'].children[1];
  fileFirst.onmouseenter(); await flush();
  assert.deepEqual(preloaded, []);
  fileFirst.onmouseleave();

  await normal.onclick();
  await fileFirst.onclick();
  assert.deepEqual(opened, ['normal']);
  assert.deepEqual(guides, ['pp-labs']);
  assert.equal(ids['plugin-workbench-panel'].open, false);
  ids['plugin-workbench-entry'].onclick(); await settle();
  ids['plugin-workbench-close'].onclick();
  assert.equal(ids['plugin-workbench-panel'].open, false);

  ids['plugin-workbench-updates'].onclick(); await settle();
  assert.deepEqual(upgraded, ['normal']);
  assert.equal(ids['plugin-workbench-update-count'].hidden, true);

  ids['plugin-workbench-autostart'].onclick();
  assert.equal(ids['plugin-autostart-dialog'].open, true);
  assert.equal(ids['plugin-autostart-empty'].hidden, true);
  assert.equal(ids['plugin-autostart-list'].children.length, 2);
  const normalRow = ids['plugin-autostart-list'].children[0];
  assert.equal(normalRow.children[1].attributes['aria-pressed'], 'false');
  await normalRow.children[1].onclick(); await settle();
  assert.equal(apiCalls[0].path, '/plugins/normal/autostart');
  assert.equal(JSON.parse(apiCalls[0].options.body).enabled, true);
  assert.equal(plugins[0].autoStart, true);
  assert.equal(ids['plugin-autostart-list'].children[0].children[1].attributes['aria-pressed'], 'true');
  ids['plugin-autostart-close'].onclick();
  assert.equal(ids['plugin-autostart-dialog'].open, false);

  const html = fs.readFileSync('internal/server/web/index.html', 'utf8');
  const entry = html.match(/<button id="plugin-workbench-entry"[\s\S]*?<\/button>/)?.[0] || '';
  assert.ok(entry && !entry.includes('<span'));
  assert.match(entry, /aria-label="工作台面板"/);
  assert.match(html, /id="plugin-workbench-letters"/);
  assert.match(html, /id="plugin-workbench-preview"/);
  assert.match(html, /id="plugin-workbench-updates"/);
  assert.match(html, /id="plugin-autostart-dialog"/);
  assert.match(html, /id="plugin-autostart-enabled"/);
  assert.ok(html.indexOf('/plugin-workbench-launcher.js') < html.indexOf('/app.js'));
  const css = fs.readFileSync('internal/server/web/plugin-workbench-launcher.css', 'utf8');
  assert.ok(!css.includes('backdrop-filter'), 'modal scrolling must not continuously reblur the application behind it');
  assert.ok(!css.includes('scroll-behavior:smooth'), 'letter navigation must not start a long smooth-scroll animation');
  assert.match(css, /\.workbench-plugin-preview\{position:absolute;z-index:20;/, 'details must stay above card and heading layers');
  console.log('PASS launcher safe hover, stable letter navigation, update badge/upgrade-all, autostart management and opening states');
})().catch(error => { console.error(error); process.exitCode = 1; });
