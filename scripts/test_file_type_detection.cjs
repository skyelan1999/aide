'use strict';

const assert = require('node:assert/strict');
const { effectiveTypePath, extension } = require('../internal/server/web/file-types.js');

for (const [input, expected] of [
  ['target.REQ-001.md.v0.10.bak', 'target.REQ-001.md'],
  ['target.REQ-001.md.pre-d7.bak', 'target.REQ-001.md'],
  ['target.REQ-001.md.pre-v04.bak', 'target.REQ-001.md'],
  ['成果.xlsx.bak', '成果.xlsx'],
  ['C:\\workspace\\图纸.PDF.BAK', 'C:\\workspace\\图纸.PDF'],
  ['nested.bak.name.md', 'nested.bak.name.md'],
  ['double.xlsx.bak.bak', 'double.xlsx.bak'],
]) {
  assert.equal(effectiveTypePath(input), expected, input);
}

assert.equal(extension('target.REQ-001.md.v0.10.bak'), 'md');
assert.equal(extension('成果.xlsx.bak'), 'xlsx');
assert.equal(extension('archive.zip.bak'), 'zip');
assert.equal(extension('.env.bak'), '');
console.log('file type detection PASS');
