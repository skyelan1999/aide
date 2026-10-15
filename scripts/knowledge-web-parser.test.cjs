'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const {execFileSync} = require('node:child_process');
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
const wrapped = parse(`const $=id=>document.getElementById(id);
async function api(path,options={}){const headers={...options.headers};const response=await fetch('/api'+path,{...options,headers});return response.json();}
async function save(){return api('/save',{method:'POST'});}
$('save').onclick=save;`);
assert.equal(wrapped.events[0].target,'save');
assert.equal(wrapped.events[0].callback,'save');
assert.deepEqual(wrapped.requests.map(r=>[r.caller,r.url,r.method,r.wrapper,r.wrapperLine]),[['save','/api/save','POST','api',2]]);
for(const text of [
  `function api(path,options={}){options.method='DELETE';return fetch('/api'+path,{...options});}api('/save',{method:'POST'});`,
  `function api(path,options={}){const alias=options;alias.method='DELETE';return fetch('/api'+path,{...options});}api('/save');`,
  `function api(path,options={}){mutate(options);return fetch('/api'+path,{...options});}api('/save');`,
  `function api(path,options={}){return fetch('/api'+path,{...options,method:'DELETE'});}api('/save');`,
  `function api(path,options={}){return fetch('/api'+path,{...options});}function run(api){api('/fake');}`,
  `function api(path,options={}){eval("path='/fake'");return fetch('/api'+path,{...options});}api('/save');`,
  `function api(path,options={}){arguments[0]='/fake';return fetch('/api'+path,{...options});}api('/save');`,
  `function api(path,options={}){options.mutate();return fetch('/api'+path,{...options});}api('/save');`,
])assert.equal(parse(text).requests.length,0,'unsafe or shadowed wrapper invented a request');
const isolated = parse(`function api(path,options={}){return fetch('/api'+path,{...options});} function one(){api('/one');}function two(){api('/two',{method:'POST'});}`);
assert.deepEqual(isolated.requests.map(r=>[r.caller,r.url]),[['one','/api/one'],['two','/api/two']]);
assert.equal(parse("const $=id=>document.getElementById(id);document.title='new';$('save').onclick=()=>{};").events.length,1);
const actualStarMap=parse(fs.readFileSync(path.join(root,'internal/server/web/starmap.js'),'utf8'));
assert.ok(actualStarMap.events.length>0,'actual star-map DOM helper not recognized');
assert.ok(actualStarMap.requests.some(r=>r.wrapper==='api'&&r.url==='/api/knowledge-map/paths'),'actual star-map API wrapper not recognized');
function parseHTML(text) {
  const source = fs.readFileSync(path.join(root, 'internal/server/code_analysis/html.py'), 'utf8');
  return JSON.parse(execFileSync('python3', ['-I', '-c', source], {input:JSON.stringify([{id:'H',text}]),encoding:'utf8',timeout:3000,maxBuffer:2<<20}))[0];
}
const page = parseHTML(`<!doctype html>\n<button id="save">保存</button>\n<script src="./app.js?v=1&amp;x=2" defer></script>`);
assert.equal(page.symbols[0].key, 'button#save');
assert.equal(page.symbols[0].line, 2);
assert.equal(page.scripts[0].path, './app.js?v=1&x=2');
assert.equal(page.scripts[0].line, 3);
const inert = parseHTML(`<template><button id="inert">x</button></template><textarea><button id="text">x</button></textarea><!--<button id="comment">x</button>--><script>throw new Error('must not execute');</script><button id="live">x</button>`);
assert.deepEqual(inert.symbols.map(s=>s.name), ['#live']);
assert.ok(inert.unresolvedWeb.some(r=>r.reason==='inline_script_unsupported'));
const duplicate = parseHTML(`<button id="same">x</button><div id="same"></div><button id="a" id="b">x</button><button id="a">ambiguous</button>`);
assert.equal(duplicate.symbols.length, 0);
assert.ok(duplicate.unresolvedWeb.some(r=>r.reason==='duplicate_dom_id'));
assert.ok(duplicate.unresolvedWeb.some(r=>r.reason==='duplicate_html_attributes'));
assert.equal(parseHTML(`<base href="https://example.test/"><button id="save">x</button>`).htmlBase, true);
const htmlBound = parseHTML(Array.from({length:300},(_,i)=>`<button id="b${i}"></button><script src="s${i}.js"></script>`).join(''));
assert.equal(htmlBound.symbols.length, 256);
assert.equal(htmlBound.scripts.length, 256);
assert.equal(htmlBound.webReferencesTruncated, true);
console.log('PASS JS DOM callback/request evidence, lexical boundaries, dynamic diagnostics and bounds');
console.log('PASS HTML button/script declarations, inactive markup, ambiguity, base URL and bounds');
