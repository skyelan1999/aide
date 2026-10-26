'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
function source() {return app.slice(app.indexOf('const DXF_SVG_NS ='), app.indexOf('/* ── docx-preview 渲染器'));}
async function fixture(doc, response) {
  let svgNodes = 0, parses = 0;
  class Element {
    constructor(tag, cls = '', text = '') {this.tag = tag;this.className = cls;this.textContent = text;this.children = [];this.style = {};this.classList = {add() {}, remove() {}};}
    append(...nodes) {this.children.push(...nodes);}
    appendChild(node) {this.append(node);return node;}
    set innerHTML(_value) {this.children = [];}
    setAttribute() {}
    closest() {return null;}
  }
  const context = {console, Response, TextDecoder, Uint8Array, ArrayBuffer, Set, Map,
    t: value => value, escapeHtml: value => value, el: (...args) => new Element(...args),
    fileRawUrl: () => '/owned.dxf', ensureDxfParser: async () => {},
    DxfParser: class {parseSync() {parses++;return doc;}}, fetch: async () => response || new Response('0\nSECTION\n0\nENDSEC\n0\nEOF\n'),
    document: {createElementNS(_ns, tag) {svgNodes++;return new Element(tag);}, body: {classList: {contains: () => false}}},
    window: {addEventListener() {}, removeEventListener() {}}, bindViewerTeardown() {}, history: {back() {}}};
  vm.createContext(context);
  if (fs.existsSync('internal/server/web/preview-safety.js')) vm.runInContext(fs.readFileSync('internal/server/web/preview-safety.js', 'utf8'), context);
  vm.runInContext(source(), context);const container = new Element('div');context.ownedContainer = container;
  await vm.runInContext('setupDxfPreview(ownedContainer,"owned.dxf","workspace","")', context);
  const all = [];function walk(node) {all.push(node);for (const child of node.children) walk(child);}walk(container);
  return {all, svgNodes, parses};
}
const line = {type: 'LINE', vertices: [{x: 0, y: 0}, {x: 1, y: 1}]};
async function main() {
  const cycle = await fixture({entities: [{type: 'INSERT', name: 'A'}], blocks: {A: {entities: [{type: 'DIMENSION', block: 'B'}]}, B: {entities: [{type: 'INSERT', name: 'A'}]}}});
  assert.ok(cycle.all.some(node => node.className === 'dxf-error'), 'cyclic INSERT/DIMENSION must produce explicit preview error');
  assert.equal(cycle.all.some(node => node.tag === 'svg'), false, 'partial SVG must not remain after rejection');assert.ok(cycle.svgNodes < 100);
  const blocks = {};
  for (let i = 0; i < 40; i++) blocks['B' + i] = {entities: i === 39 ? [line] : [{type: i % 2 ? 'DIMENSION' : 'INSERT', name: 'B' + (i + 1), block: 'B' + (i + 1)}]};
  assert.ok((await fixture({entities: [{type: 'INSERT', name: 'B0'}], blocks})).all.some(node => node.className === 'dxf-error'), 'deep mixed block chain must reject');
  const shared = await fixture({entities: [{type: 'INSERT', name: 'shared'}, {type: 'DIMENSION', block: 'shared'}], blocks: {shared: {entities: [line]}}});
  assert.equal(shared.all.filter(node => node.tag === 'line').length, 2, 'valid repeated block is not a cycle');
  assert.equal(shared.all.some(node => node.className === 'dxf-error'), false);
  const expansion = {};
  for (let i = 0; i < 18; i++) expansion['E' + i] = {entities: i === 17 ? [line] : [{type: 'INSERT', name: 'E' + (i + 1)}, {type: 'DIMENSION', block: 'E' + (i + 1)}]};
  const fanout = await fixture({entities: [{type: 'INSERT', name: 'E0'}], blocks: expansion});
  assert.ok(fanout.all.some(node => node.className === 'dxf-error'));assert.ok(fanout.svgNodes <= 50000, 'DOM expansion must stay bounded');
  const vertices = Array.from({length: 100}, (_, i) => ({x: i, y: i % 3}));
  const geometry = await fixture({entities: Array.from({length: 3000}, () => ({type: 'INSERT', name: 'poly'})), blocks: {poly: {entities: [{type: 'LWPOLYLINE', vertices}]}}});
  assert.ok(geometry.all.some(node => node.className === 'dxf-error'), 'shared geometry work budget must bound repeated point expansion');
  const labels = await fixture({entities: Array.from({length: 200}, () => ({type: 'INSERT', name: 'label'})), blocks: {label: {entities: [{type: 'TEXT', text: 'x'.repeat(10000)}]}}});
  assert.ok(labels.all.some(node => node.className === 'dxf-error'), 'few nodes with repeated large text must not bypass memory budget');
  const longLabel = await fixture({entities: [{type: 'TEXT', text: 'x'.repeat(16385)}]});
  assert.ok(longLabel.all.some(node => node.className === 'dxf-error'), 'single oversized text run must reject before DOM assignment');
  let canceled = false;
  const hugeHeader = new Response(new ReadableStream({cancel() {canceled = true;}}), {headers: {'Content-Length': String(5 * 1024 * 1024)}});
  const oversized = await fixture({entities: [line]}, hugeHeader);
  assert.equal(oversized.parses, 0);assert.equal(canceled, true, 'oversized response must cancel before read/parse');
  const tooManyLines = await fixture({entities: [line]}, new Response('0\n'.repeat(100001)));
  assert.equal(tooManyLines.parses, 0, 'line scan must reject parser-heavy input before parseSync');
  console.log('PASS actual DXF renderer cycle/depth/fanout/node/geometry guards, repeated valid blocks, input cancellation and pre-parser line bound');
}
module.exports = {source};
if (require.main === module) main().catch(error => {console.error(error);process.exitCode = 1;});
