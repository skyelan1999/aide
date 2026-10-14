const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const start = source.indexOf('function markdownSourceAnchors(');
const end = source.indexOf('\nfunction setSplitScrollLock(', start);
assert(start >= 0 && end > start, 'source-anchor and scroll-sync functions must exist');

const tokens = [
  {raw: '# First section\n\n'},
  {raw: 'A long paragraph.\nsecond line\nthird line\n'},
  {raw: '## Next section\n'}
];
const rect = top => ({top, bottom: top + 100, height: 100});
const nodes = [
  {dataset: {}, getBoundingClientRect: () => rect(100)},
  {dataset: {}, getBoundingClientRect: () => ({top: 220, bottom: 520, height: 300})},
  {dataset: {}, getBoundingClientRect: () => rect(550)}
];
const preview = {
  scrollTop: 0, clientTop: 0, _markdownSourceAnchors: [],
  querySelector: () => ({children: nodes}),
  getBoundingClientRect: () => ({top: 100})
};
const editor = {scrollTop: 30, scrollHeight: 1000, clientHeight: 100};
const context = {
  window: {marked: {lexer: () => tokens}},
  getComputedStyle: target => target === editor ? {lineHeight: '10px', fontSize: '10px'} : {paddingTop: '0px'},
  $: id => id === 'editor' ? editor : id === 'editor-preview' ? preview : null,
  splitScrollLock: {editor: true, fileView: false},
  splitScrollExpected: new WeakMap(),
  splitScrollLast: {editor: 'editor', fileView: 'file-view-editor'},
  Math, String, Number, parseFloat
};
vm.runInNewContext(source.slice(start, end), context);
preview._markdownSourceAnchors = context.markdownSourceAnchors('# source', preview);
assert.deepEqual(Array.from(preview._markdownSourceAnchors, a => [a.startLine, a.endLine]), [[0, 2], [2, 5], [5, 6]]);
assert.deepEqual(nodes.map(node => node.dataset.sourceLine), ['1', '3', '6']);

context.syncSplitScroll(editor, preview, 'editor');
assert.equal(preview.scrollTop, 220, 'editor position within a multi-line block maps proportionally into its rendered block');
preview.scrollTop = 220;
context.syncSplitScroll(preview, editor, 'editor');
assert.equal(editor.scrollTop, 30, 'preview progress within the same block maps back to the matching source lines');

// If preview layout changes, the next editor-driven sync must use the new
// rendered block geometry while retaining the source-line progress.
nodes[1].getBoundingClientRect = () => ({top: 260, bottom: 660, height: 400});
preview.scrollTop = 0;
context.splitScrollExpected = new WeakMap();
context.syncSplitScroll(editor, preview, 'editor');
assert.ok(Math.abs(preview.scrollTop - 293.3333333333333) < 1e-9, 'reflowed preview blocks are realigned using proportional position');

console.log('PASS: Markdown source anchors, bidirectional within-block progress, and reflow alignment');
