'use strict';
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium } = require('./browser-runtime/node_modules/playwright');

(async () => {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert(out, 'AIDE_COMPONENT_EVIDENCE_DIR is required');
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch({
    executablePath: process.env.AIDE_CHROMIUM_PATH ||
      (process.platform === 'linux' ? '/usr/bin/chromium' : chromium.executablePath()),
    args: ['--no-sandbox', '--disable-dev-shm-usage'],
  });
  try {
    const page = await browser.newPage();
    await page.goto('data:text/html,<meta charset="utf-8"><main></main>');
    const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
    const start = source.indexOf('const BLOCKED_TAGS =');
    const end = source.indexOf('\n/* draw.io embed', start);
    assert(start >= 0 && end > start, 'could not locate Markdown sanitizer');
    const result = await page.evaluate((sanitizer) => {
      (0, eval)(sanitizer);
      const input = '<div style="position:fixed;inset:0;z-index:2147483647"><a style="display:block;width:100vw;height:100vh" href="https://example.test/">open</a><img src="x" onerror="alert(1)"></div>';
      const body = sanitizeHtml(input);
      return {
        html: body.innerHTML,
        styles: body.querySelectorAll('[style]').length,
        handlers: body.querySelectorAll('[onerror]').length,
        links: body.querySelectorAll('a[href]').length,
      };
    }, source.slice(start, end));
    assert.equal(result.styles, 0, 'inline styles must be removed');
    assert.equal(result.handlers, 0, 'event handlers must be removed');
    assert.equal(result.links, 1, 'ordinary safe links should remain');
    fs.writeFileSync(path.join(out, 'result.json'), JSON.stringify(result, null, 2) + '\n');
    console.log(JSON.stringify(result));
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
