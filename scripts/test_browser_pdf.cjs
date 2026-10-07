'use strict';
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const { pdfText } = require('../plugins/browser-control/index')._test;
async function main() {
  const script = 'import base64,io\nfrom pypdf import PdfWriter\nw=PdfWriter()\nw.add_blank_page(width=100,height=100)\nw.add_blank_page(width=100,height=100)\nw.encrypt("", algorithm="AES-256")\nb=io.BytesIO()\nw.write(b)\nprint(base64.b64encode(b.getvalue()).decode())';
  const fixture = spawnSync('python3', ['-I', '-c', script], { encoding: 'utf8', timeout: 10000 });
  assert.equal(fixture.status, 0, fixture.stderr);
  const result = await pdfText(fixture.stdout.trim());
  assert.equal(result.pages, 2); assert.equal(result.pagesRead, 2); assert.equal(result.truncated, false);
  const last = await pdfText(fixture.stdout.trim(), 2);
  assert.equal(last.fromPage, 2); assert.equal(last.pagesRead, 1);
  await assert.rejects(pdfText(fixture.stdout.trim(), 3), /fromPage exceeds/);
  await assert.rejects(pdfText(Buffer.from('not a PDF').toString('base64')), /Invalid/);
  console.log('PASS PDF: AES text extraction, page continuation, out-of-range and magic rejection');
}
main().catch(error => { console.error(error); process.exitCode = 1; });
