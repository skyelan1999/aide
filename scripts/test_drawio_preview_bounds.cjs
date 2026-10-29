'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
const pako = require('../internal/server/web/vendor/drawio/js/deflate/pako.min.js');
const bundle = fs.readFileSync('internal/server/web/vendor/drawio/js/app.min.js', 'utf8');
function runtime() {
  let parsed = 0, indexed = 0, adopted = 0;
  function Codec() {}
  Codec.prototype.updateElements = function () {indexed++;};
  function Model() {}
  Model.prototype.setRoot = function (cell) {adopted++;this.root = cell;return cell;};
  function ObjectCodec() {}
  ObjectCodec.prototype.decode = function (depth) {return depth ? this.decode(depth - 1) : 'decoded';};
  const root = {Graph: {stringToArrayBuffer: text => Uint8Array.from(text, c => c.charCodeAt(0)), zapGremlins: text => text},
    mxUtils: {parseXml() {parsed++;return {documentElement: null};}}, mxCodec: Codec, mxGraphModel: Model, mxObjectCodec: ObjectCodec,
    pako, atob, TextDecoder, location: {origin: 'https://owned.invalid'}};
  vm.runInNewContext(bundle.slice(bundle.indexOf('Graph.decompress=function'), bundle.indexOf('Graph.fadeNodes=function')), {Graph: root.Graph, pako, atob, decodeURIComponent});
  if (fs.existsSync('internal/server/web/drawio-preview-safety.js')) {
    root.AideDrawioSafety = require('../internal/server/web/drawio-preview-safety.js')(root);root.AideDrawioSafety.install();
  }
  return {root, get parsed() {return parsed;}, get indexed() {return indexed;}, get adopted() {return adopted;}};
}
function compressed(text) {return Buffer.from(pako.deflateRaw(encodeURIComponent(text))).toString('base64');}
function cell(children = []) {return {children, getChildCount() {return children.length;}, getChildAt(i) {return children[i];}};}
function limit(error) {return error.code === 'AIDE_DRAWIO_LIMIT';}
function main(mode = 'all') {
  if (mode === 'all' || mode === 'inflate') {
    const f = runtime(), big = compressed('x'.repeat(8 * 1024 * 1024 + 1));
    assert.throws(() => f.root.Graph.decompress(big), limit, 'actual bundled Graph.decompress must reject expanded data before accumulating it');
    const normal = '<mxGraphModel><root><mxCell id="0"/></root></mxGraphModel>';
    assert.equal(f.root.Graph.decompress(compressed(normal)), normal);
    assert.equal(f.root.Graph.decompress(''), '');
    assert.equal(f.root.Graph.decompress(null), null);
    assert.equal(f.root.Graph.decompress(Buffer.from(pako.deflate(encodeURIComponent(normal))).toString('base64'), true), normal);
    assert.equal(f.root.Graph.decompress(compressed('x'.repeat(8 * 1024 * 1024))).length, 8 * 1024 * 1024);
    let consumed = 0;
    f.root.pako = {...pako, Inflate: class extends pako.Inflate {push(data, final) {consumed += data.length;return super.push(data, final);}}};
    const bomb = compressed('x'.repeat(32 * 1024 * 1024));
    assert.throws(() => f.root.Graph.decompress(bomb), limit);
    assert.ok(consumed < Buffer.from(bomb, 'base64').length, 'inflation must stop before consuming remaining compressed chunks');
  }
  if (mode === 'all' || mode === 'xml') {
    const f = runtime(), deep = '<n>'.repeat(129) + '</n>'.repeat(129);
    assert.throws(() => f.root.mxUtils.parseXml(deep), limit, 'deep XML must reject before native parser and recursive index');
    assert.equal(f.parsed, 0);
    f.root.mxUtils.parseXml('<n attr="a > b"><![CDATA[<n><n>]]><!-- <n> --></n>');
    assert.equal(f.parsed, 1, 'comments/CDATA/quoted delimiters must not count as nesting');
    for (const xml of ['<n>'.repeat(129)+'</n>'.repeat(129), '<n>'+'<n/>'.repeat(50000)+'</n>',
      '<!DOCTYPE n [<!ENTITY value "expanded">]><n>&value;</n>', '<n>'+'界'.repeat(3*1024*1024)+'</n>']) {
      assert.throws(()=>f.root.mxUtils.parseXml(xml), limit);
    }
    assert.equal(f.parsed, 1, 'all rejected before native parser');
  }
  if (mode === 'all' || mode === 'cells') {
    const f = runtime();let root = cell();for (let i = 0; i < 128; i++) root = cell([root]);
    assert.throws(() => new f.root.mxGraphModel().setRoot(root), limit, 'deep actual cell tree must reject before recursive root registration');
    assert.equal(f.adopted, 0);
    const normal = cell([cell(),cell([cell()])]);assert.equal(new f.root.mxGraphModel().setRoot(normal), normal);
    const guard = f.root.AideDrawioSafety;
    const parents = Array.from({length:129}, (_, i) => ({id: String(i), parent: i ? String(i-1) : null}));
    assert.throws(() => guard.checkParents(parents), limit);
    assert.throws(() => guard.checkParents([{id:'a',parent:'b'},{id:'b',parent:'a'}]), limit);
    guard.checkParents([{id:'a',parent:null},{id:'b',parent:'a'},{id:'c',parent:'a'}]);
    guard.checkParents(parents.slice(0,128));
    const shared = cell();assert.throws(()=>guard.checkRoot(cell([shared,shared])),limit);
    const cycle = cell();cycle.children.push(cycle);assert.throws(()=>guard.checkRoot(cycle),limit);
    const flat = cell(Array.from({length:9999},()=>cell()));guard.checkRoot(flat);
    flat.children.push(cell());assert.throws(()=>guard.checkRoot(flat),limit);
    const codec = new f.root.mxObjectCodec();assert.throws(()=>codec.decode(128),limit);
    assert.equal(codec.decode(127),'decoded','failed decode must unwind depth budget');
  }
  console.log('PASS draw.io ' + mode + ' boundaries (bundled decompressor plus runtime-hook fixtures)');
}
module.exports = {compressed, cell};
if (require.main === module) {try {main(process.argv[2] || 'all');} catch (error) {console.error(error.message);process.exitCode = 1;}}
