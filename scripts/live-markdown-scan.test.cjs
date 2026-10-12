const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const factory = source.match(/function newLiveBoundaryState\(\)[\s\S]*?\n}/)?.[0];
const scanner = source.match(/function advanceLiveBoundary\(src, scan\)[\s\S]*?\n}/)?.[0];
assert.ok(factory && scanner, 'incremental boundary scanner is present');
const context = {};
vm.createContext(context);
vm.runInContext(`${factory}; ${scanner}; this.newLiveBoundaryState = newLiveBoundaryState; this.advanceLiveBoundary = advanceLiveBoundary;`, context);

function referenceSplit(src) {
  const lines = src.split('\n');
  let fenceOpen = false, fenceStart = -1, lastFenceEnd = -1;
  for (let i = 0; i < lines.length; i++) {
    if (/^ {0,3}(```|~~~)/.test(lines[i])) {
      if (!fenceOpen) { fenceOpen = true; fenceStart = i; }
      else { fenceOpen = false; lastFenceEnd = i; }
    }
  }
  const offset = k => lines.slice(0, k).reduce((n, line) => n + line.length + 1, 0);
  if (fenceOpen) return offset(fenceStart);
  const lower = lastFenceEnd >= 0 ? lastFenceEnd : 0;
  for (let i = lines.length - 1; i >= lower; i--) if (!lines[i].trim()) return offset(i);
  return offset(lower);
}

for (const text of [
  'one paragraph',
  'first\n\nsecond\n\nthird',
  'before\n\n```go\nline one\n\nline two\n```\n\nafter',
  '~~~json\n{\n  "a": 1\n}\n~~~\n\nnext',
  '   ```lang\ncode\n```',
]) {
  const state = context.newLiveBoundaryState();
  for (let i = 0; i < text.length; i += 3) context.advanceLiveBoundary(text.slice(0, i + 3), state);
  assert.equal(context.advanceLiveBoundary(text, state), referenceSplit(text), `split mismatch: ${text}`);
}

const large = 'x'.repeat(2_000_000);
const state = context.newLiveBoundaryState();
for (let i = 0; i < large.length; i += 4096) context.advanceLiveBoundary(large.slice(0, i + 4096), state);
assert.equal(state.scanned, large.length);
console.log('live markdown scan regression: PASS');
