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

// A direct user preview navigates the existing tab, preserves the return map,
// and never requests a popup (the native Safari failure mode).
const navigated=[];const direct=c.window.AideStarmapPreview.create({origin:'https://aide.test',workspace:()=> 'work-1',locked:()=>false,key:'direct-owner',note(){},navigate:url=>navigated.push(url),returnTo:()=>'/starmap.html?channel=existing',openWindow(){throw Error('popup must never be requested');}});
assert.equal(direct.open(href(spec)),true);let directURL=new URL(navigated[0]);assert.equal(directURL.searchParams.get('starmapReturn'),'/starmap.html?channel=existing');assert.deepEqual(JSON.parse(decodeURIComponent(directURL.hash.slice(6))),spec);
assert.equal(direct.open(href({...spec,path:'next.md'})),true);assert.equal(new URL(navigated[1]).searchParams.get('previewRequest'),'2');
const hostile=c.window.AideStarmapPreview.create({origin:'https://aide.test',workspace:()=> 'work-1',locked:()=>false,key:'safe-return',note(){},navigate:url=>navigated.push(url),returnTo:()=> 'https://elsewhere.test/starmap.html'});hostile.open(href(spec));assert.equal(new URL(navigated[2]).searchParams.get('starmapReturn'),'/starmap.html');
assert.equal(direct.open(href({...spec,knowledgeWorkspace:'other'})),false);assert.equal(navigated.length,3);

// Return navigation never accepts a different origin/path or forwards secrets.
const fileRouteSource=fs.readFileSync('internal/server/web/file-route.js','utf8');
for(const [value,expected] of [['/starmap.html?channel=owned&code=1&access_token=secret','/starmap.html?channel=owned&code=1'],['https://other.test/starmap.html',null],['javascript:alert(1)',null],['/admin',null],['/starmap.html?channel=bad%20channel','/starmap.html']]){
 const routeContext=vm.createContext({window:{},URL,URLSearchParams,location:{origin:'https://aide.test',search:'?starmapReturn='+encodeURIComponent(value)}});vm.runInContext(fileRouteSource,routeContext);assert.equal(routeContext.window.AideFileRoute.returnToMap(),expected);
}

// Document text locations use the server's structured line and raw-file hash,
// never infer a line from translated page/paragraph/cell locator text.
const elements=new Map();locator.$=id=>{if(!elements.has(id))elements.set(id,{});return elements.get(id);};locator.select=()=>{};locator.syncNodeSourceChrome=()=>{};
vm.runInContext(source.slice(source.indexOf('function selectDocument('),source.indexOf('function renderDocumentResults(')),locator);
const plainFile={id:'Fdoc',kind:'file',root:'context',source:'ssh-reference',path:'docs/guide.md',origin:'origin-1',format:'.md',contentHash:'indexed-prefix'};
vm.runInContext('selectDocument('+JSON.stringify({file:plainFile,line:51,locator:'text line 51',offset:0,hash:'a'.repeat(64),text:'exact excerpt'})+')',locator);
const documentSpec=JSON.parse(decodeURIComponent(elements.get('document-open').href.split('=')[1]));assert.equal(documentSpec.line,51);assert.equal(documentSpec.endLine,51);assert.equal(documentSpec.codeHash,'a'.repeat(64));assert.equal(documentSpec.source,'ssh-reference');assert.equal(documentSpec.readOnly,true);
vm.runInContext('selectDocument('+JSON.stringify({file:{...plainFile,format:'.docx'},locator:'paragraph 51',offset:0,hash:'b'.repeat(64),text:'office paragraph'})+')',locator);
const officeSpec=JSON.parse(decodeURIComponent(elements.get('document-open').href.split('=')[1]));assert.equal(officeSpec.line,undefined);assert.equal(officeSpec.codeHash,undefined);
console.log('PASS: same-tab read-only preview, safe map return, structured line/hash, legacy window ownership, lock/workspace and stale-route guards');
