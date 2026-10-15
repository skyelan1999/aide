'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const c=vm.createContext({window:{},URL,URLSearchParams});vm.runInContext(fs.readFileSync('internal/server/web/starmap-preview.js','utf8'),c);
const spec={root:'context',source:'ssh-reference',path:'docs/guide.md',origin:'origin-1',readOnly:true,knowledgeWorkspace:'work-1',format:'.md',line:42,endLine:46,codeHash:'hash-1'};
const href=spec=>'/#file='+encodeURIComponent(JSON.stringify(spec));let locked=false,workspace='work-1',opened=[],notes=[],blocked=false,pending=false;
function makeWindow(url){let here=new URL(url);return {closed:false,opener:{},focuses:0,focus(){this.focuses++},close(){this.closed=true},get location(){const win=this;return {get href(){return here.href},get origin(){return here.origin},replace(url){here=new URL(url);win.navigations=(win.navigations||0)+1}}},navigate(url){here=new URL(url)}};}
const owner=c.window.AideStarmapPreview.create({origin:'https://aide.test',workspace:()=>workspace,locked:()=>locked,key:'owned-preview',note:x=>notes.push(x),openWindow(url){if(blocked)return null;const win=makeWindow(pending?'about:blank':url);opened.push({url,win});return win;}});
assert.equal(owner.open(href(spec)),true);assert.equal(opened.length,1);assert.equal(opened[0].win.opener,null);
const first=new URL(opened[0].url);assert.deepEqual(JSON.parse(new URLSearchParams(first.hash.slice(1)).get('file')),spec);assert.equal(first.searchParams.has('access_token'),false);
assert.equal(owner.open(href({...spec,path:'other.md'})),true);assert.equal(opened.length,1);assert.equal(opened[0].win.navigations,1);assert.equal(opened[0].win.focuses,1);assert.equal(new URL(opened[0].win.location.href).searchParams.get('previewRequest'),'2');
owner.close();assert.equal(opened[0].win.closed,true);
pending=true;owner.open(href(spec));owner.open(href({...spec,path:'fast.md'}));assert.equal(opened.length,2,'fast second click reuses not-yet-loaded owned window');pending=false;
opened[1].win.navigate('https://elsewhere.test/');owner.close();assert.equal(opened[1].win.closed,false,'preserve user-navigated window');
owner.open(href(spec));opened[2].win.navigate(opened[2].url.replace(encodeURIComponent('"readOnly":true'),encodeURIComponent('"readOnly":false')));owner.close();assert.equal(opened[2].win.closed,false,'preserve editable user route');
blocked=true;assert.equal(owner.open(href(spec)),false);assert.match(notes.at(-1),/允许/);blocked=false;
for(const invalid of [href({...spec,readOnly:false}),href({...spec,origin:''}),href({...spec,knowledgeWorkspace:'other'}),'https://other.test/'+href(spec),'javascript:alert(1)'])assert.equal(owner.open(invalid),false);
locked=true;assert.equal(owner.open(href(spec)),false);locked=false;workspace='other';assert.equal(owner.open(href(spec)),false);
const source=fs.readFileSync('internal/server/web/starmap.js','utf8'),html=fs.readFileSync('internal/server/web/starmap.html','utf8');
assert.match(html,/id="node-open" data-file-preview/);assert.match(source,/if\(ref\.hit\)selectDocument\(ref\.hit\);else select\(node\);openNodePreview\(ref\.hit\?/);assert.match(source,/if\(id==='ai-found'\)openNodePreview\(hit\.file\)/);
// The actual locator builder must preserve origin, source, format and line data.
const locator=vm.createContext({graph:{workspace:'work-1'}});vm.runInContext(source.slice(source.indexOf('function pathSourceLink('),source.indexOf('function renderEvidencePath(')),locator);
const built=vm.runInContext(`pathSourceLink(${JSON.stringify({id:'C1',kind:'symbol',...spec,contentHash:spec.codeHash})},43)`,locator),actual=JSON.parse(decodeURIComponent(built.split('=')[1]));assert.equal(actual.source,spec.source);assert.equal(actual.origin,spec.origin);assert.equal(actual.line,43);assert.equal(actual.endLine,43);assert.equal(actual.codeHash,spec.codeHash);assert.equal(actual.readOnly,true);
assert.equal(vm.runInContext('pathSourceLink({kind:"directory",path:"docs"})',locator),null);
console.log('PASS: source-aware read-only routes, reuse/full navigation, pending window, popup failure, lock/workspace and preservation of user-navigated/editable pages');
