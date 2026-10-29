// Run from repository root. Executes the actual scanner without modifying it.
const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync('internal/server/web/app.js', 'utf8');
const start = src.indexOf('function newLiveBoundaryState()');
const end = src.indexOf('// A large open Markdown block', start);
if (start < 0 || end < start) throw Error('Scanner extraction anchors changed');
const context = {};
vm.createContext(context);
vm.runInContext(src.slice(start, end), context);
for (const eol of ['\n', '\r\n']) {
  const scan = context.newLiveBoundaryState();
  const input = ['```js', 'const a = 1;', '', 'const b = 2;'].join(eol) + eol;
  const boundary = context.advanceLiveBoundary(input, scan);
  if (!scan.fenceOpen || boundary !== 0) throw Error('Fence split: ' + JSON.stringify(eol));
  console.log(JSON.stringify({ eol, fenceOpen: scan.fenceOpen, boundary }));
}
