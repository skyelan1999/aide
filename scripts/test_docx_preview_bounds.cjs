'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
const JSZip = require('../internal/server/web/vendor/jszip.min.js');
function docxSource() {
  const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
  return app.slice(app.indexOf('async function setupDocxPreview('), app.indexOf('window.AideMarkdownCode?.install'));
}
async function zip(files) {
  const archive = new JSZip();for (const [name, data] of Object.entries(files)) archive.file(name, data);
  return archive.generateAsync({type: 'uint8array', compression: 'DEFLATE'});
}
function sizes(input, size) {
  const bytes = input.slice(), view = new DataView(bytes.buffer);
  for (let offset = 0; offset <= bytes.length - 46; offset++) if (view.getUint32(offset, true) === 0x02014b50) view.setUint32(offset + 24, size, true);
  return bytes;
}
async function renderFixture(bytes, response, zipImpl = JSZip) {
  let rendered = 0;
  class Element {
    constructor(tag, cls = '', text = '') {this.tag = tag;this.className = cls;this.textContent = text;this.children = [];this.style = {};this.classList = {toggle() {}};}
    append(...nodes) {this.children.push(...nodes);}
    remove() {}
    set innerHTML(value) {this.children = [];this.html = value;}
    closest() {return null;}
  }
  const context = {console, Response, Uint8Array, ArrayBuffer, DataView, TextDecoder, setTimeout, clearTimeout,
    teardownViewer() {}, bindViewerTeardown() {}, isDocPath: () => false, isBackupPath: () => false, t: value => value, escapeHtml: value => value,
    el: (...args) => new Element(...args), fileRawUrl: () => '/owned.docx', fetch: async () => response || new Response(bytes),
    document: {createElement: tag => new Element(tag), body: {classList: {contains: () => false}}}, history: {back() {}},
    window: {AideDocxStyles: {apply:()=>()=>{}}, JSZip: zipImpl, docx: {async renderAsync() {rendered++;}}}};
  vm.createContext(context);vm.runInContext(fs.readFileSync('internal/server/web/preview-safety.js', 'utf8'), context);
  vm.runInContext(docxSource(), context);const container = new Element('div');context.ownedContainer = container;
  await vm.runInContext('setupDocxPreview(ownedContainer,"owned.docx","workspace","",{readOnly:true,origin:"owned"})', context);
  return {rendered, container};
}
async function main() {
  const normal = await zip({'[Content_Types].xml': '<Types/>', '_rels/.rels': '<Relationships/>', 'word/document.xml': '<document/>'});
  const declaredBomb = sizes(normal, 64 * 1024 * 1024);
  assert.equal((await renderFixture(declaredBomb)).rendered, 0, 'declared oversized OOXML part must reject before docx renderer');
  const safety = require('../internal/server/web/preview-safety.js');
  await safety.checkDocx(normal, JSZip);assert.equal((await renderFixture(normal)).rendered, 1, 'normal package must retain renderer path');
  let canceled = false;
  const oversized = new Response(new ReadableStream({cancel() {canceled = true;}}), {headers: {'Content-Length': String(17 * 1024 * 1024)}});
  assert.equal((await renderFixture(normal, oversized)).rendered, 0);assert.equal(canceled, true);
  const source = new Uint8Array(32 * 1024 * 1024), lying = sizes(await zip({'word/document.xml': source}), 1024);
  let maxProgress = 0, emitted = 0;
  const observedZip = {version: JSZip.version, async loadAsync(input) {
    const loaded = await JSZip.loadAsync(input);
    for (const entry of Object.values(loaded.files)) {
      const original = entry.internalStream.bind(entry);
      entry.internalStream = type => {
        const stream = original(type), on = stream.on;
        stream.on = function (event, callback) {
          return on.call(this, event, event === 'data' ? function (data, metadata) {emitted += data.length;maxProgress = Math.max(maxProgress, metadata.percent);callback.call(this, data, metadata);} : callback);
        };return stream;
      };
    }
    return loaded;
  }};
  await assert.rejects(safety.checkDocx(lying, observedZip), error => error.code === 'AIDE_PREVIEW_LIMIT' && error.message.includes('解压内容'));
  assert.ok(maxProgress < 100, 'paused preflight must not consume the whole compressed stream');
  assert.ok(emitted <= 17 * 1024 * 1024, 'only bounded current input chunk may finish decoding after pause');
  assert.equal((await renderFixture(lying)).rendered, 0, 'lying central sizes must not reach renderer');
  const many = {};for (let i = 0; i < 513; i++) many['part-' + i + '.xml'] = '';
  let loaded = 0;
  await assert.rejects(safety.checkDocx(await zip(many), {version: JSZip.version, loadAsync() {loaded++;}}));
  assert.equal(loaded, 0, 'central-directory count must reject before JSZip metadata allocation');
  const combined = {};for (let i = 0; i < 5; i++) combined['part-' + i + '.xml'] = new Uint8Array(7 * 1024 * 1024);
  const combinedBytes = await zip(combined);
  await assert.rejects(safety.checkDocx(combinedBytes, JSZip), error => error.message.includes('解压内容'));
  // Fault-inject loader-normalized lengths so actual aggregate accounting is
  // exercised independently of JSZip's normal declared-size mismatch rejection.
  const actualAggregate = {version: JSZip.version, async loadAsync(input) {
    const loaded = await JSZip.loadAsync(input);
    for (const entry of Object.values(loaded.files)) entry._data.uncompressedSize = 7 * 1024 * 1024;
    return loaded;
  }};
  await assert.rejects(safety.checkDocx(sizes(combinedBytes, 1024), actualAggregate), error => error.message.includes('解压内容'));
  await safety.checkDocx(await zip({'part.xml': new Uint8Array(8 * 1024 * 1024)}), JSZip);
  let pulls = 0, stopped = false;
  const unknownLength = new Response(new ReadableStream({pull(controller) {
    if (++pulls <= 64) controller.enqueue(new Uint8Array(1024 * 1024));else controller.close();
  }, cancel() {stopped = true;}}));
  await assert.rejects(safety.readBytes(unknownLength, safety.limits.docxBytes));
  assert.equal(stopped, true);assert.ok(pulls <= 18, 'unknown-size download must stop at limit without buffering full response');
  await assert.rejects(safety.checkDocx(new Uint8Array([1, 2, 3]), JSZip));
  await assert.rejects(safety.checkDocx(normal, {version: 'unexpected', loadAsync: JSZip.loadAsync}));
  let clock = 1000, paused = false, watchdog;
  const timerContext = {window: {}, Uint8Array, DataView, Date: {now: () => clock}, clearTimeout,
    setTimeout(fn, delay) {return setTimeout(() => {clock += delay;fn();}, 1);}};
  vm.createContext(timerContext);vm.runInContext(fs.readFileSync('internal/server/web/preview-safety.js', 'utf8'), timerContext);
  const stalled = {version: JSZip.version, async loadAsync() {return {files: {owned: {internalStream() {return {
    on() {return this;}, resume() {return this;}, pause() {paused = true;return this;}
  };}}}};}};
  try {
    await assert.rejects(Promise.race([timerContext.window.AidePreviewSafety.checkDocx(normal, stalled),
      new Promise((_, reject) => {watchdog = setTimeout(() => reject(Error('preflight deadline did not fire')), 1000);})]), error => error.message.includes('解压内容'));
  } finally {clearTimeout(watchdog);}
  assert.equal(paused, true);assert.equal(clock, 11000, 'fake clock must exercise full ten-second stream deadline');
  console.log('PASS actual DOCX viewer rejects compressed/declared/actual per-part/aggregate/count/malformed limits before renderer; valid package retained; real JSZip pause stops further input');
}
module.exports = {docxSource, zip, sizes};
if (require.main === module) main().catch(error => {console.error(error.message);process.exitCode = 1;});
