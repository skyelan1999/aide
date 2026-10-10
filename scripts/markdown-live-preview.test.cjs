const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const start = source.indexOf('function bindMarkdownLivePreview(');
const end = source.indexOf('\nfor(const [editorId,previewId,panelId,getSpec]', start);
assert(start >= 0 && end > start, 'actual live-preview function must exist');
function fixture(panelId = 'file-view') {
  const listeners = {}, hidden = new Set(), timers = new Map(), renders = [];
  const editor = {value: 'recovered', addEventListener: (type, fn) => {listeners[type] = fn;}};
  const host = {scrollTop: 42, classList: {contains: name => hidden.has('preview:' + name)}};
  const panel = {open: true, classList: {contains: name => hidden.has('panel:' + name)}};
  const lockScreen = {locked: false};
  let spec = {path: 'notes.md'}, serial = 0, disposed = 0;
  const context = {
    $: id => id === 'editor' ? editor : id === 'preview' ? host : panel,
    lockScreen, isMarkdownPath: p => /\.(md|markdown)$/i.test(p || ''),
    window: {AideMarkdownOutline: {dispose: () => disposed++}},
    setupMarkdownPreview: (h, text, p, s) => {renders.push({text, p, s}); h.scrollTop = 0;},
    setTimeout: fn => {timers.set(++serial, fn); return serial;},
    clearTimeout: id => timers.delete(id)
  };
  vm.runInNewContext(source.slice(start, end), context);
  context.bindMarkdownLivePreview('editor', 'preview', panelId, () => spec);
  return {editor, host, panel, hidden, lockScreen, renders, setSpec: s => {spec = s;},
    input: composing => listeners.input({isComposing: composing}),
    compositionEnd: () => listeners.compositionend(),
    flush: () => {const work = [...timers.values()]; timers.clear(); work.forEach(fn => fn());},
    disposed: () => disposed};
}
for (const split of [false, true]) {
  const f = fixture(); if (split) f.hidden.add('panel:md-split');
  f.input(false); f.editor.value = 'latest'; f.input(false); f.flush();
  assert.equal(f.renders.length, 1); assert.equal(f.renders[0].text, 'latest');
  assert.equal(f.host.scrollTop, 42); assert.equal(f.disposed(), 1);
}
for (const hide of ['preview:hidden', 'panel:hidden']) {
  const f = fixture(); f.hidden.add(hide); f.input(false); f.flush(); assert.equal(f.renders.length, 0);
}
for (const mutate of [f => f.setSpec({path: 'other.md'}), f => f.hidden.add('preview:hidden'), f => {f.lockScreen.locked = true;}]) {
  const f = fixture(); f.input(false); mutate(f); f.flush(); assert.equal(f.renders.length, 0);
}
const modal = fixture('editor-dialog'); modal.panel.open = false; modal.input(false); modal.flush(); assert.equal(modal.renders.length, 0);
const text = fixture(); text.setSpec({path: 'code.txt'}); text.input(false); text.flush(); assert.equal(text.renders.length, 0);
const hinted = fixture(); hinted.setSpec({path: 'virtual', format: '.md'}); hinted.input(false); hinted.flush(); assert.equal(hinted.renders.length, 1);
const ime = fixture(); ime.input(true); ime.flush(); assert.equal(ime.renders.length, 0); ime.compositionEnd(); ime.flush(); assert.equal(ime.renders.length, 1);
console.log('PASS: Markdown preview recovery, split, debounce, scroll, visibility, stale context, lock and IME');
