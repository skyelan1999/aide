'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const diff = require('../internal/server/web/file-diff.js');

test('marks replacements with line numbers on both sides', () => {
  const result = diff.compute('a\nb\nc\n', 'a\nB\nc\n');
  assert.deepEqual(result.rows.map(row => [row.kind, row.text, row.oldNo, row.newNo], ), [
    ['ctx', 'a', 1, 1],
    ['del', 'b', 2, null],
    ['add', 'B', null, 2],
    ['ctx', 'c', 3, 3],
  ]);
  assert.equal(result.adds, 1);
  assert.equal(result.dels, 1);
  assert.equal(result.truncated, 0);
});

test('treats a new file as pure additions starting at line 1', () => {
  const result = diff.compute('', 'one\ntwo\n');
  assert.deepEqual(result.rows, [
    {kind: 'add', text: 'one', oldNo: null, newNo: 1},
    {kind: 'add', text: 'two', oldNo: null, newNo: 2},
  ]);
  assert.equal(result.adds, 2);
  assert.equal(result.dels, 0);
});

test('normalizes CRLF and keeps blank lines addressable', () => {
  const result = diff.compute('x\r\n\r\ny\r\n', 'x\n\nz\ny\n');
  assert.deepEqual(result.rows.map(row => [row.kind, row.text]), [
    ['ctx', 'x'], ['ctx', ''], ['add', 'z'], ['ctx', 'y'],
  ]);
  assert.equal(result.rows[2].newNo, 3);
});

test('keeps the whole file when nothing changed', () => {
  const result = diff.compute('same\nfile\n', 'same\nfile\n');
  assert.deepEqual(result.rows.map(row => row.kind), ['ctx', 'ctx']);
  assert.equal(result.adds + result.dels, 0);
});

test('degrades to delete-all plus add-all for oversized middles', () => {
  const before = Array.from({length: 900}, (_, i) => 'old-' + i).join('\n');
  const after = Array.from({length: 900}, (_, i) => 'new-' + i).join('\n');
  const result = diff.compute(before, after);
  assert.equal(result.adds, 900);
  assert.equal(result.dels, 900);
  assert.ok(result.rows.length <= 2000);
});

test('caps rendered rows but reports the real counts', () => {
  const before = Array.from({length: 3000}, (_, i) => 'a' + i).join('\n');
  const after = Array.from({length: 3000}, (_, i) => 'b' + i).join('\n');
  const result = diff.compute(before, after);
  assert.equal(result.rows.length, 2000);
  assert.ok(result.truncated > 0);
});
