'use strict';
// Positive control: the packaged decoder must retain identical pixels for
// a normal boxed image and its unboxed JPEG 2000 codestream.
const fs = require('node:fs');
const assert = require('node:assert/strict');
const {test} = require('node:test');
const preflight = require('./pdf-jpx-preflight.cjs');

test('standards-conforming JP2 and raw J2K retain matching RGB pixels', async () => {
  const sample = Buffer.from(JSON.parse(fs.readFileSync('scripts/fixtures/pdf-jpx-components.json')).images[0].base64, 'base64');
  let raw, headers = 0;
  for (let i = 0; i < sample.length;) {
    const length = sample.readUInt32BE(i);
    assert(length >= 8 && i + length <= sample.length);
    const type = sample.toString('ascii', i + 4, i + 8);
    if (type === 'jp2h') headers++;
    if (type === 'jp2c') raw = sample.subarray(i + 8, i + length);
    i += length;
  }
  assert.equal(headers, 1);
  assert(raw);
  assert.equal(preflight(sample), sample);
  assert.equal(preflight(raw), raw);
  const worker = fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs', 'utf8');
  const {JpxImage} = await import('data:text/javascript;base64,' + Buffer.from(worker + ';export {JpxImage};').toString('base64'));
  const boxed = JpxImage.decode(sample, {numComponents: 3});
  assert(boxed instanceof Uint8ClampedArray);
  assert.equal(boxed.length, 48);
  assert.deepEqual(boxed, JpxImage.decode(raw, {numComponents: 3}));
});
