'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const start = source.indexOf('function formatTransferBytes(');
const end = source.indexOf('// uploadCollected', start);
assert.ok(start >= 0 && end > start, 'upload progress helpers should exist');

let ticks = [10, 110];
let responseStatus = 200;
let responseText = '{"ok":true}';
class MockXHR {
  constructor() { this.upload = {}; this.headers = {}; }
  open(method, url) { this.method = method; this.url = url; }
  setRequestHeader(name, value) { this.headers[name] = value; }
  send(body) {
    this.body = body;
    this.upload.onprogress({ loaded: 512, total: 1024, lengthComputable: true });
    this.upload.onload();
    this.status = responseStatus;
    this.responseText = responseText;
    this.onload();
  }
}

const sandbox = {
  state: { token: 'test-token' },
  BATCH_MAX_FILES: 2,
  BATCH_MAX_BYTES: 10,
  performance: { now: () => ticks.shift() ?? 110 },
  XMLHttpRequest: MockXHR,
};
vm.runInNewContext(source.slice(start, end), sandbox);
assert.equal(sandbox.formatTransferBytes(1536), '1.5 KB');
const countLimited = sandbox.splitUploadChunks([1, 2, 3].map(id => ({ file: { size: 1 }, id })));
assert.deepEqual(Array.from(countLimited, chunk => chunk.items.length), [2, 1]);
const byteLimited = sandbox.splitUploadChunks([6, 5, 4].map((size, id) => ({ file: { size }, id })));
assert.deepEqual(Array.from(byteLimited, chunk => chunk.items.length), [1, 2]);

(async () => {
  const updates = [];
  const result = await sandbox.uploadWithProgress('/upload', 'body', (...args) => updates.push(args), 'text/plain');
  assert.equal(result.ok, true);
  assert.equal(result.data.ok, true);
  assert.equal(updates.some(x => x[0] === 512 && x[1] === 1024 && x[2] === 5120 && x[3] === '正在传输'), true);
  assert.deepEqual(updates.at(-1).slice(0, 2), [512, 1024], 'waiting-for-server state preserves completed byte progress');
  assert.equal(updates.at(-1)[3], '数据已发送，等待服务器确认');

  responseStatus = 403;
  responseText = '{"error":"该来源为只读"}';
  const failed = await sandbox.uploadWithProgress('/upload', 'body', () => {});
  assert.equal(failed.ok, false);
  assert.equal(failed.data.error, '该来源为只读');
  console.log('upload progress PASS');
})().catch(error => { console.error(error); process.exitCode = 1; });
