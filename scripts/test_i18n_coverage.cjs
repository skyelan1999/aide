'use strict';
// Only explicit product translation sites are audited. User data and protocol values are not rewritten.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const root = path.join(__dirname, '../internal/server/web');
const context = {window:{}};
vm.runInNewContext(fs.readFileSync(path.join(root, 'locales/en.js'), 'utf8'), context);
const catalog = context.window.aideEnglish;
const seenKeys = new Set();
for (const match of fs.readFileSync(path.join(root, 'locales/en.js'), 'utf8').matchAll(/^\s*("(?:\\.|[^"\\])*")\s*:/gm)) {
  const key = JSON.parse(match[1]);
  assert(!seenKeys.has(key), 'Shadowed catalog key: ' + key);
  seenKeys.add(key);
}
const missing = new Map();
const decode = s => s.replace(/&quot;/g, '"').replace(/&#39;|&apos;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
let sites = 0;
function inspect(key, file) {
  if (!/[\u3400-\u9fff]/u.test(key)) return;
  sites++;
  if (!Object.hasOwn(catalog, key)) missing.set(key, file);
}
for (const name of fs.readdirSync(root).filter(name => /\.(?:js|html)$/.test(name))) {
  const source = fs.readFileSync(path.join(root, name), 'utf8');
  if (name.endsWith('.js')) {
    const calls = /\b(?:t|tr|translate|uiText[A-Z]\w*)\s*\(\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')/g;
    for (const match of source.matchAll(calls)) {
      // The regexp accepts a single quoted literal, never executable expressions.
      inspect(vm.runInNewContext(match[1], {}, {timeout:100}), name);
    }
  }
  for (const match of source.matchAll(/\bdata-i18n(?:-(?:title|placeholder|aria-label|data-prompt))?="([^"]*)"/g)) inspect(decode(match[1]), name);
}
const schema = JSON.parse(fs.readFileSync(path.join(root, 'settings-schema.json'), 'utf8'));
function walk(value) {
  if (Array.isArray(value)) return value.forEach(walk);
  if (!value || typeof value !== 'object') return;
  for (const [key, item] of Object.entries(value)) {
    if (['title','label','description'].includes(key) && typeof item === 'string') inspect(item, 'settings-schema.json');
    else walk(item);
  }
}
walk(schema);
for (const [key, translated] of Object.entries(catalog)) {
  assert.equal(typeof translated, 'string', key);
  const slots = s => [...s.matchAll(/\{(?:\d+|[a-zA-Z_]\w*)\}/g)].map(x => x[0]).sort();
  assert.deepEqual(slots(translated), slots(key), 'Translation placeholders: ' + key);
}
if (missing.size) {
  console.error([...missing].map(([key,file]) => file + ': ' + JSON.stringify(key)).join('\n'));
  throw Error(missing.size + ' explicit product keys have no English translation');
}
console.log('PASS: ' + sites + ' explicit UI translation sites, settings schema and all placeholder sets');
