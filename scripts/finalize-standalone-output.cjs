'use strict';

// Keep standalone runtime files in their original classic-script/CommonJS form.
// TypeScript otherwise adds module markers to .cts and forced-module .ts outputs.
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const configs = ['tsconfig.standalone-browser.json', 'tsconfig.standalone-node.json'];
let finalized = 0;

for (const name of configs) {
  const configPath = path.join(__dirname, name);
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
  for (const file of config.files || []) {
    if (file.endsWith('.d.ts')) continue;
    const sourcePath = path.resolve(path.dirname(configPath), file);
    const runtimePath = sourcePath.endsWith('.cts')
      ? sourcePath.slice(0, -4) + '.cjs'
      : sourcePath.slice(0, -3) + '.js';
    const source = fs.readFileSync(sourcePath, 'utf8');
    let runtime = fs.readFileSync(runtimePath, 'utf8');
    const original = runtime;
    runtime = runtime.replace(/^Object\.defineProperty\(exports, ["']__esModule["'], \{ value: true \}\);\r?\n/m, '');
    const sourceHasStrict = /^(?:#![^\n]*\n)?\s*(?:'use strict'|"use strict");/.test(source);
    if (!sourceHasStrict) runtime = runtime.replace(/^(?:'use strict'|"use strict");\r?\n/, '');
    if (runtime !== original) {
      fs.writeFileSync(runtimePath, runtime);
      finalized++;
    }
  }
}

console.log(`Standalone output finalized: ${finalized} files adjusted`);
