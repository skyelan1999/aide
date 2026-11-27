'use strict';
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium, webkit } = require('./browser-runtime/node_modules/playwright');

(async () => {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert(out, 'AIDE_COMPONENT_EVIDENCE_DIR is required');
  fs.mkdirSync(out, { recursive: true });
  const browser = process.argv.includes('--webkit') ? await webkit.launch() : await chromium.launch({
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
      const links = sanitizeHtml([
        '<a href="https://example.test/guide" target="_blank" rel="OpEnEr help">Guide</a>',
        '<a href="https://example.test/notes" target="notes">Notes</a>',
        '<a href="https://example.test/home" target="_self" rel="help">Home</a>',
        '<a href="./README.md">Readme</a>',
      ].join('')).querySelectorAll('a');
      const controls = sanitizeHtml([
        '<label for="fixture"><strong>Checklist</strong></label>',
        '<input type="checkbox" checked form="fixture"><input type="checkbox">',
        '<input type="text" form="fixture"><button form="fixture" commandfor="fixture" command="show-modal"><em>Review</em></button>',
        '<p autofocus accesskey="q" contenteditable="true" popover>Notes</p>',
      ].join(''));
      return {
        html: body.innerHTML,
        styles: body.querySelectorAll('[style]').length,
        handlers: body.querySelectorAll('[onerror]').length,
        links: body.querySelectorAll('a[href]').length,
        anchors: Array.from(links, a => ({href:a.getAttribute('href'), target:a.target, rel:Array.from(a.relList).sort()})),
        checkboxes: Array.from(controls.querySelectorAll('input'), box => [box.type, box.disabled, box.checked]),
        activeControls: controls.querySelectorAll('button, label, [form], [command], [commandfor], [autofocus], [accesskey], [contenteditable], [popover]').length,
        formattedLabels: Array.from(controls.querySelectorAll('strong, em'), node => node.textContent),
      };
    }, source.slice(start, end));
    assert.equal(result.styles, 0, 'inline styles must be removed');
    assert.equal(result.handlers, 0, 'event handlers must be removed');
    assert.equal(result.links, 1, 'ordinary safe links should remain');
    assert.deepEqual(result.anchors, [
      {href:'https://example.test/guide', target:'_blank', rel:['help','noopener','noreferrer']},
      {href:'https://example.test/notes', target:'notes', rel:['noopener','noreferrer']},
      {href:'https://example.test/home', target:'_self', rel:['help']},
      {href:'./README.md', target:'', rel:[]},
    ]);
    assert.deepEqual(result.checkboxes, [['checkbox',true,true], ['checkbox',true,false]]);
    assert.equal(result.activeControls, 0);
    assert.deepEqual(result.formattedLabels, ['Checklist','Review']);
    fs.writeFileSync(path.join(out, 'result.json'), JSON.stringify(result, null, 2) + '\n');
    console.log(JSON.stringify(result));
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
