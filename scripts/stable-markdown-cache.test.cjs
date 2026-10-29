const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const start = source.indexOf('const originalMarkdownRender=renderMarkdown;');
const end = source.indexOf('/* ── 语音小秘', start);
assert(start >= 0 && end > start, 'stable Markdown cache wrapper should be present');

const rendered = [];
let mermaidScheduled = 0;
const context = {
  window: {marked:{parse(){}}},
  rendered,
  renderMarkdown(text) { rendered.push(text); return `html:${text}`; },
  scheduleMermaid() { mermaidScheduled++; },
};
vm.createContext(context);
vm.runInContext(`${source.slice(start, end)}\n globalThis.renderStable = renderMarkdown; globalThis.clearStable = clearStableMarkdownCache; globalThis.cacheEntries = () => stableMarkdownCache.size; globalThis.cacheBytes = () => stableMarkdownCacheBytes;`, context);

assert.equal(context.renderStable('same'), 'html:same');
assert.equal(context.renderStable('same'), 'html:same');
assert.equal(context.rendered.length, 1, 'unchanged completed Markdown should reuse its rendered HTML');

context.renderStable('live', true);
context.renderStable('live', true);
context.renderStable('relative image', false, 'docs/readme.md');
context.renderStable('relative image', false, 'docs/readme.md');
context.renderStable('viewer image', false, '', { root: 'workspace' });
context.renderStable('viewer image', false, '', { root: 'workspace' });
assert.equal(context.rendered.length, 7, 'live or path-aware content must bypass the context-free cache');

const mermaid = '```mermaid\ngraph TD; a-->b\n```';
context.renderStable(mermaid);
context.renderStable(mermaid);
assert.equal(mermaidScheduled, 2, 'cached Mermaid markup must still schedule rendering');

context.clearStable();
assert.equal(context.cacheEntries(), 0);
assert.equal(context.cacheBytes(), 0);
for (let i = 0; i < 8; i++) context.renderStable(`${i}:` + 'x'.repeat(150_000));
assert(context.cacheEntries() < 8, 'least-recent entries should be evicted under the memory cap');
assert(context.cacheBytes() <= 2 * 1024 * 1024, 'cache memory estimate must stay bounded');

const tailStart = source.indexOf('function updatePlainLiveTail(');
const tailEnd = source.indexOf('\nfunction renderLiveAnswer(', tailStart);
assert(tailStart >= 0 && tailEnd > tailStart, 'large live Markdown tails should have an incremental renderer');
const appendedLengths = [], createdLengths = [];
class FakeTextNode {
  constructor(text) { this.nodeType = 3; this.text = text; createdLengths.push(text.length); }
  appendData(text) { appendedLengths.push(text.length); this.text += text; }
}
const tailContext = {
  Node: { TEXT_NODE: 3 },
  document: { createTextNode: text => new FakeTextNode(text) },
};
vm.createContext(tailContext);
vm.runInContext(`${source.slice(tailStart, tailEnd)}\nglobalThis.updateTail = updatePlainLiveTail;`, tailContext);
const active = {
  dataset: {}, children: [],
  get firstChild() { return this.children[0] || null; },
  replaceChildren(node) { this.children = [node]; },
};
const longPrefix = 'x'.repeat(20_000);
tailContext.updateTail(active, longPrefix, 0);
tailContext.updateTail(active, longPrefix + 'abc', 0);
assert.deepEqual(createdLengths, [20_000], 'the first visible large tail is materialized once');
assert.deepEqual(appendedLengths, [3], 'later frames append only newly received text');
tailContext.updateTail(active, longPrefix + '\n\nnext tail', longPrefix.length + 2);
assert.equal(active.firstChild.text, 'next tail', 'when a block closes, the new active offset gets a fresh text node');
tailContext.updateTail(active, longPrefix + '\n\nx', longPrefix.length + 2);
assert.equal(active.firstChild.text, 'x', 'rewound active text resets instead of retaining stale suffixes');

context.clearStable();
context.window.marked=null;
const beforeRecovery=context.rendered.length;
context.renderStable('await renderer');context.renderStable('await renderer');
assert.equal(context.rendered.length,beforeRecovery+2,'plain fallback is never cached');
context.window.marked={parse(){}};
context.renderStable('await renderer');context.renderStable('await renderer');
assert.equal(context.rendered.length,beforeRecovery+3,'restored renderer caches only its formatted output');
context.window.marked={parse(){}};
context.renderStable('await renderer');
assert.equal(context.rendered.length,beforeRecovery+4,'a replaced parser invalidates old HTML');

console.log('PASS: stable Markdown reuse, plain fallback bypass, renderer replacement, bounded eviction and live tails');
