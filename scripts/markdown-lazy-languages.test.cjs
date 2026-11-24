'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const { test } = require('node:test');

function fixture() {
  const scripts = [];
  const engine = {
    loaded: false,
    getLanguage(language) { return language === 'cmake' && this.loaded ? {} : null; },
    highlight(text) { return { value: `<span class="hljs-keyword">${text}</span>`, language: 'cmake', relevance: 10 }; },
    highlightAuto(text) { return { value: text, language: undefined, relevance: 0 }; },
  };
  const document = {
    createElement(tag) {
      assert.equal(tag, 'script');
      return { async: true, onload: null, onerror: null, src: '' };
    },
    head: { appendChild(script) { scripts.push(script); } },
  };
  const context = { window: { hljs: engine }, document, console, setTimeout, clearTimeout };
  context.window.window = context.window;
  vm.createContext(context);
  vm.runInContext(fs.readFileSync('internal/server/web/markdown-code.js', 'utf8'), context);
  const code = () => ({
    className: 'language-cmake', classList: { add() {} }, dataset: {}, isConnected: true,
    textContent: 'project(sample)', innerHTML: '',
  });
  return { api: context.window.AideMarkdownCode, scripts, engine, code };
}

test('rare grammars share one request and still produce exact highlighted text', async () => {
  const { api, scripts, engine, code } = fixture();
  const first = code(), second = code();
  const body = { querySelectorAll(selector) { return selector === 'pre > code' ? [first, second] : []; } };
  const pending = api.decorate(body);
  assert.equal(scripts.length, 1, 'concurrent blocks share one grammar request');
  assert.match(scripts[0].src, /\/vendor\/highlight\.js\/languages\/cmake\.min\.js\?v=67$/);
  engine.loaded = true;
  scripts[0].onload();
  await pending;
  for (const block of [first, second]) {
    assert.equal(block.textContent, 'project(sample)');
    assert.equal(block.dataset.highlightLanguage, 'cmake');
    assert.match(block.innerHTML, /hljs-keyword/);
  }
  await api.decorate(body);
  assert.equal(scripts.length, 1, 'loaded grammars never trigger another request');
});

test('initial HTML defers grammars and heavy document libraries', () => {
  const index = fs.readFileSync('internal/server/web/index.html', 'utf8');
  const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
  assert(!index.includes('/vendor/highlight.js/languages/'));
  assert(!index.includes('<script src="/vendor/jszip.min.js'));
  assert(!index.includes('<script src="/vendor/docx-preview/docx-preview.min.js'));
  assert(app.includes("ensureVendorScript('/vendor/jszip.min.js?v=67'"));
  assert(app.includes("ensureVendorScript('/vendor/docx-preview/docx-preview.min.js?v=67'"));
});
