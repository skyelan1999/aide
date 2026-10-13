const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const read = (path) => fs.readFileSync(path, 'utf8');
const app = read('internal/server/web/app.js');
const start = app.indexOf('function codeLang(path) {');
const end = app.indexOf('\nfunction fileRenderEnabled', start);
assert(start >= 0 && end > start, 'codeLang stays an isolated file-viewer helper');
const context = { AideFileTypes: { extension(path) { return String(path).split('.').pop(); } } };
vm.createContext(context);
vm.runInContext(`${app.slice(start, end)}; this.codeLang = codeLang;`, context);
for (const path of ['CMakeLists.txt', 'cmake/Project.cmake', 'toolchain.cmake.in']) {
  assert.equal(context.codeLang(path), 'cmake', `${path} is identified as CMake`);
}
assert.equal(context.codeLang('src/main.cpp'), 'cpp', 'existing extension mappings remain available');

const hljs = {};
const highlightContext = vm.createContext({ hljs });
vm.runInContext(read('internal/server/web/vendor/highlight.js/highlight.min.js'), highlightContext);
vm.runInContext(read('internal/server/web/vendor/highlight.js/languages/cmake.min.js'), highlightContext);
const sample = [
  'cmake_minimum_required(VERSION 3.25)',
  'set(SDK "$ENV{SDK_ROOT}/${CMAKE_SOURCE_DIR}")',
  'add_executable(app main.cpp)',
  '# local target',
  '#[[multi-line',
  'comment block]]',
].join('\n');
const rendered = highlightContext.hljs.highlight(sample, { language: 'cmake' }).value;
for (const token of ['hljs-keyword', 'hljs-string', 'hljs-variable', 'hljs-comment']) {
  assert(rendered.includes(token), `CMake highlighting includes ${token}`);
}
const index = read('internal/server/web/index.html');
assert(index.includes('/vendor/highlight.js/languages/cmake.min.js?v=1'), 'CMake grammar is included locally');
console.log('PASS: CMakeLists.txt, .cmake, .cmake.in identification and local syntax highlighting');
