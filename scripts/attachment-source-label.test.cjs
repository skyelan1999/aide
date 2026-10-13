const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const start = source.indexOf('function renderAttachments() {');
const end = source.indexOf('\nasync function loadFiles', start);
assert.ok(start >= 0 && end > start, 'renderAttachments definition must remain extractable');

const host = {children: [], replaceChildren(...children) { this.children = children; }, append(child) { this.children.push(child); }};
const elements = new Map([['attachment-chips', host]]);
const state = {sources: [{id: 'docs-a', name: 'Design docs'}], attachments: [
  {root: 'source', source: 'docs-a', path: '/shared/readme.md'},
  {root: 'source', source: 'missing-source', path: '/shared/readme.md', directory: true},
  {root: 'workspace', path: 'readme.md'},
]};
function el(tag, className, text) {
  return {tag, className, textContent: String(text || ''), attrs: {}, children: [], title: '',
    append(child) { this.children.push(child); },
    setAttribute(name, value) { this.attrs[name] = value; }};
}
const context = {
  state, el, $: id => elements.get(id), scheduleChatRecovery() {}, scheduleContextPreview() {},
  t: (key, ...args) => key.replace(/\{(\d+)\}/g, (_match, index) => args[Number(index)] ?? _match),
};
vm.runInNewContext(source.slice(start, end) + '\nrenderAttachments();', context);

assert.equal(host.children.length, 3);
assert.equal(host.children[0].textContent, '引用源 · Design docs · /shared/readme.md');
assert.equal(host.children[0].title, host.children[0].textContent);
assert.equal(host.children[0].children[0].attrs['aria-label'], '移除附件 引用源 · Design docs · /shared/readme.md');
assert.equal(host.children[1].textContent, '▱ 引用源 · missing-source · /shared/readme.md');
assert.equal(host.children[1].children[0].attrs['aria-label'], '移除附件 引用源 · missing-source · /shared/readme.md');
assert.equal(host.children[2].textContent, 'readme.md', 'workspace attachments retain their compact label');
console.log('Recovered attachment labels retain source identity, distinguish same-path sources, and remain accessible PASS');
