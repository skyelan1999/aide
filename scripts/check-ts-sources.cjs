'use strict';

const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const runtimeFiles = [];

function walk(directory, relative = '') {
  for (const entry of fs.readdirSync(path.join(directory, relative), { withFileTypes: true })) {
    const rel = path.posix.join(relative, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== 'vendor' && entry.name !== 'node_modules') walk(directory, rel);
      continue;
    }
    if (/\.(?:js|ts)$/.test(entry.name) && !entry.name.endsWith('.d.ts')) runtimeFiles.push(rel);
  }
}

walk(path.join(root, 'internal/server/web'));
for (const name of ['computer-bridge', 'computer-bridge-linux', 'headless-browser-bridge', 'safari-bridge']) {
  runtimeFiles.push(path.posix.join('scripts', name + '.js'), path.posix.join('scripts', name + '.ts'));
}
for (const name of ['tsconfig.standalone-browser.json', 'tsconfig.standalone-node.json']) {
  const configPath = path.join(root, 'scripts', name);
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
  for (const file of config.files || []) {
    if (file.endsWith('.d.ts')) continue;
    const source = path.relative(root, path.resolve(path.dirname(configPath), file)).split(path.sep).join('/');
    const runtime = source.endsWith('.cts') ? source.slice(0, -4) + '.cjs' : source.slice(0, -3) + '.js';
    runtimeFiles.push(source, runtime);
  }
}

const files = new Set(runtimeFiles);
const errors = [];
for (const rel of files) {
  if (rel.endsWith('.js') && !files.has(rel.slice(0, -3) + '.ts')) errors.push(rel + ' has no TypeScript source');
  if (rel.endsWith('.cjs') && !files.has(rel.slice(0, -4) + '.cts')) errors.push(rel + ' has no TypeScript source');
  if (rel.endsWith('.ts') && !files.has(rel.slice(0, -3) + '.js')) errors.push(rel + ' has no generated JavaScript artifact');
  if (rel.endsWith('.cts') && !files.has(rel.slice(0, -4) + '.cjs')) errors.push(rel + ' has no generated JavaScript artifact');
}
if (errors.length) {
  console.error(errors.join('\n'));
  process.exitCode = 1;
} else {
  const count = runtimeFiles.filter(name => /\.(?:js|cjs)$/.test(name)).length;
  console.log(`TS source coverage: ${count} runtime JavaScript files paired`);
}
