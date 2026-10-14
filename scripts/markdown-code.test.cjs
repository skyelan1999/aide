'use strict';
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const source = fs.readFileSync('internal/server/web/markdown-code.js','utf8');
function fixture(clipboard, copied = true) {
  const events = [], fields = [], writes = [], timers = new Map(); let serial = 0;
  const active = {isConnected:true, selectionStart:2, selectionEnd:4, selectionDirection:'forward', focus(){this.restored=true;},setSelectionRange(...range){this.range=range;}};
  const modal = {append(field){fields.push(field);field.modal=true;}};
  const document = {activeElement:active,body:{append:field=>fields.push(field)},createElement:() => ({style:{},focus(){},select(){},setSelectionRange(){},remove(){this.removed=true;}}),execCommand(command){assert.equal(command,'copy');writes.push(fields.at(-1).value);return copied;},addEventListener(type,fn,capture){assert.equal(type,'click');assert.equal(capture,true);events.push(fn);}};
  const context = {window:{navigator:{clipboard}},document,console,clearTimeout:id=>timers.delete(id),setTimeout:fn=>{timers.set(++serial,fn);return serial;}};
  vm.createContext(context);
  vm.runInContext(fs.readFileSync('internal/server/web/vendor/highlight.js/highlight.min.js','utf8'),context);
  for(const lang of ['javascript','typescript','python','go','json','bash','sql','xml','css','yaml','cpp','java','rust','markdown']) vm.runInContext(fs.readFileSync(`internal/server/web/vendor/highlight.js/languages/${lang}.min.js`,'utf8'),context);
  context.window.hljs=context.hljs;vm.runInContext(source,context);
  function button(text) {
    const classes=new Set(), attrs={};
    const pre={querySelector:()=>({textContent:text}),textContent:text};
    const b={parentElement:{querySelector:()=>pre},isConnected:true,closest:()=>modal,classList:{add:c=>classes.add(c),remove:c=>classes.delete(c)},setAttribute:(k,v)=>attrs[k]=v};
    return {button:b,classes,attrs,event:{target:{closest:()=>b},preventDefault(){this.prevented=true;},stopPropagation(){this.stopped=true;}}};
  }
  return {api:context.window.AideMarkdownCode,window:context.window,events,fields,writes,active,button};
}
(async()=>{
 const original='  const name = "中文 <tag>";\n\n';const native=[];let errors=0;
 const f=fixture({writeText:async text=>native.push(text)});f.api.install({onError:()=>errors++});f.api.install();assert.equal(f.events.length,1);
 // Delegate receives fresh buttons created after installation, including rerendered and streamed blocks.
 for(let i=0;i<3;i++){const b=f.button(original);await f.events[0](b.event);assert.deepEqual(native.at(-1),original);assert(b.classes.has('copied'));assert(b.event.prevented&&b.event.stopped);}
 const wholeMarkdown='# Title\n\n```python\nprint(1)\n```\n';await f.api.copyText(wholeMarkdown);assert.equal(native.at(-1),wholeMarkdown);
 assert.equal(f.fields.length,0);
 f.window.navigator.clipboard.writeText=async()=>{throw Error('denied');};
 const b=f.button(original);await f.events[0](b.event);assert.equal(f.writes.at(-1),original);assert(f.fields.at(-1).modal&&f.fields.at(-1).removed);assert(f.active.restored);assert.deepEqual(f.active.range,[2,4,'forward']);assert.equal(errors,0);
 const legacy=fixture(undefined);await legacy.api.copyText(original,legacy.button(original).button);assert.equal(legacy.writes[0],original);
 const failed=fixture({writeText:async()=>{throw Error('denied');}},false);failed.api.install({onError:()=>errors++});const bad=failed.button(original);bad.classes.add('copied');await failed.events[0](bad.event);assert.equal(errors,1);assert(!bad.classes.has('copied'));assert(failed.fields.at(-1).removed);
 const code=f.api.highlight('def greet(name):\n    print("Hello", name)\n');assert.equal(code.language,'python');assert(code.value.includes('hljs-keyword'));
 const json=f.api.highlight('{"count": 2, "ok": true}');assert.equal(json.language,'json');assert.equal(json.detected,true);
 for(const [language,text] of [
  ['go','package main\nimport "fmt"\nfunc main() { fmt.Println("hello") }\n'],
  ['sql','CREATE TABLE users (id INTEGER PRIMARY KEY, name VARCHAR(100));\nSELECT * FROM users WHERE id = 1;'],
  ['xml','<section><h1>Hello</h1><p>world</p></section>']
 ]) assert.equal(f.api.highlight(text)?.language,language,language+' auto detection');
 const js=f.api.highlight('const x = 1;','javascript');assert.equal(js.language,'javascript');assert.equal(js.detected,false);
 assert.equal(f.api.highlight('const x = 1;','text'),null);
 assert.equal(f.api.highlight('ordinary words for a plain paragraph'),null);
 assert.equal(f.api.highlight('x'.repeat(65537),'javascript'),null);
 const unsafe=f.api.highlight('const html = "<img src=x onerror=alert(1)>";','javascript');assert(!unsafe.value.includes('<img'));assert(unsafe.value.includes('&lt;img'));
 const app=fs.readFileSync('internal/server/web/app.js','utf8');const render=app.slice(app.indexOf('function renderMarkdown('),app.indexOf('const fileView ='));
 assert(render.includes('AideMarkdownCode?.decorate(body, {live, t})'));assert(!render.includes('copy.onclick'),'serialized Markdown must not rely on discarded event handlers');
 assert(fs.readFileSync('internal/server/web/index.html','utf8').indexOf('/markdown-code.js?')<fs.readFileSync('internal/server/web/index.html','utf8').indexOf('/app.js?'));
 console.log('PASS: dynamic copy delegation, exact original text, modal fallback after clipboard rejection, focus/selection restoration, honest failure, real local language inference, explicit/plaintext priority, highlight budget and escaped HTML');
})().catch(error=>{console.error(error);process.exitCode=1;});
