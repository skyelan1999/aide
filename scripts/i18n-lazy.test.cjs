'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const { test } = require('node:test');

test('English catalog loads only when needed and shares one request', async () => {
  let pref = 'zh-CN', subscriber, events = 0;
  const scripts = [];
  const window = {
    aideUI: { get: () => pref, subscribe: fn => { subscriber = fn; } },
    addEventListener() {}, dispatchEvent() { events++; },
  };
  const document = {
    documentElement: {}, querySelectorAll: () => [],
    createElement: () => ({ async: true, onload: null, onerror: null, src: '' }),
    head: { append: script => scripts.push(script) },
  };
  const context = { window, document, navigator: { language: 'zh-CN' }, CustomEvent: class {} };
  vm.createContext(context);
  vm.runInContext(fs.readFileSync('internal/server/web/i18n.js', 'utf8'), context);
  const api = window.aideI18n;
  await api.ready();
  assert.equal(scripts.length, 0, 'Chinese startup does not fetch English');
  pref = 'en';
  subscriber(); subscriber();
  assert.equal(scripts.length, 1, 'concurrent language changes share the request');
  assert.equal(scripts[0].src, '/locales/en.js?v=216');
  window.aideEnglish = { '语言': 'Language' };
  scripts[0].onload();
  await api.ready();
  await Promise.resolve(); await Promise.resolve();
  assert.equal(api.t('语言'), 'Language');
  assert.equal(events, 1, 'one completed transition triggers one rerender');
  subscriber();
  await api.ready();
  assert.equal(scripts.length, 1, 'loaded English is reused');
});
