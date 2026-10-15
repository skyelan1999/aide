'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
const acorn = require(path.join(root, 'internal/server/code_analysis/acorn.cjs'));
const script = fs.readFileSync(path.join(root, 'internal/server/code_analysis/javascript.cjs'), 'utf8');
function parse(text) {
  let output = '';
  const program = script.replace('__AIDE_CODE_INPUT__', JSON.stringify(JSON.stringify([{id: 'F', text}])));
  vm.runInNewContext('const acorn=__acorn;\n' + program, {__acorn: acorn, process: {stdout: {write(value) {output += value;}}}}, {timeout: 3000});
  return JSON.parse(output)[0];
}
const unit = parse(`async function save(){return fetch('/api/save', {method:'POST'});}
document.getElementById('save').addEventListener('click', save);
document.querySelector('#refresh').onclick = () => fetch('/api/refresh');`);
assert.ok(Array.isArray(unit.events), 'DOM event evidence is missing');
assert.equal(unit.events.length, 2);
assert.equal(unit.events[0].target, 'save');
assert.equal(unit.events[0].callback, 'save');
assert.equal(unit.events[0].line, 2);
assert.equal(unit.events[1].target, 'refresh');
assert.ok(unit.symbols.some(s => s.key === unit.events[1].callback));
assert.deepEqual(unit.requests.map(r => [r.caller, r.method, r.url]), [['save', 'POST', '/api/save'], [unit.events[1].callback, 'GET', '/api/refresh']]);
for (const text of [
  `function local(document,fetch){document.getElementById('save').addEventListener('click',()=>fetch('/api/fake'));}`,
  `const fetch = custom; fetch('/api/fake'); const document=custom; document.getElementById('save').onclick=()=>{};`,
  `fetch=custom; fetch('/api/fake');`,
]) {
  const u = parse(text);
  assert.equal(u.events.length, 0, 'shadowed document invented a DOM binding');
  assert.equal(u.requests.length, 0, 'shadowed fetch invented a request');
  assert.ok(u.unresolvedWeb.length > 0, 'unsupported/shadowed web evidence has no diagnostic');
}
const dynamic = parse(`function run(url, options, callback){fetch(url);fetch('/api/x',options);document.getElementById('x').onclick=callback;}`);
assert.equal(dynamic.events.length, 0);
assert.equal(dynamic.requests.length, 0);
assert.ok(dynamic.unresolvedWeb.length >= 3);
for (const text of [
  `let callback=()=>{}; callback=other; document.getElementById('save').onclick=callback;`,
  `class Callback{}; document.getElementById('save').onclick=Callback;`,
  `function local(fetch){fetch=other;} globalThis.fetch=other; fetch('/fake');`,
  `window[key]=other; fetch('/fake');`,
  `document.getElementById=other; document.getElementById('save').onclick=()=>{};`,
]) {
  const u = parse(text);
  assert.equal(u.events.length + u.requests.length, 0, 'reassigned callback/global generated evidence');
}
const methods = parse("fetch(`/api/literal`); fetch('/api/x',{method:'put'}); fetch('/api/x',{...options}); fetch('/api/x',{method:verb});");
assert.deepEqual(methods.requests.map(r=>r.method), ['GET','PUT']);
assert.equal(methods.unresolvedWeb.length, 2);
const comments = parse(`// document.getElementById('fake').onclick=()=>fetch('/fake');\nconst text="fetch('/string')";`);
assert.equal(comments.events.length + comments.requests.length, 0);
const bounded = parse(Array.from({length:300}, (_,i)=>`fetch('/api/${i}');`).join('\n'));
assert.equal(bounded.requests.length, 256);
assert.equal(bounded.webReferencesTruncated, true);
const boundedEvents = parse(Array.from({length:300}, (_,i)=>`document.getElementById('b${i}').onclick=()=>{};`).join('\n'));
assert.equal(boundedEvents.events.length, 256);
assert.equal(boundedEvents.webReferencesTruncated, true);
console.log('PASS JS DOM callback/request evidence, lexical boundaries, dynamic diagnostics and bounds');
