'use strict';
const fs=require('fs'),vm=require('vm'),assert=require('assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
const ast=require('../internal/server/code_analysis/acorn.cjs').parse(source,{ecmaVersion:'latest'});
const pathFunction=ast.body.find(node=>node.type==='FunctionDeclaration'&&node.id.name==='resolveMarkdownImagePath');assert(pathFunction);
const context={};vm.createContext(context);vm.runInContext(source.slice(pathFunction.start,pathFunction.end),context);
const path=context.resolveMarkdownImagePath;
assert.equal(path('docs/readme.md','images/test.png'),'docs/images/test.png');
assert.equal(path('docs/readme.md','../images/test.png'),'images/test.png');
assert.equal(path('readme.md','images/a%20b.png'),'images/a b.png');
assert.equal(path('docs/readme.md','/images/test.png?raw=1#view'),'images/test.png');
assert(source.includes("fileRawUrl(resolved, viewerSpec?.root || 'workspace', viewerSpec?.source || '', viewerSpec)"));
assert(source.includes("setupMarkdownPreview($('editor-preview'), $('editor').value, state.file?.path || '', state.file)"));
console.log('PASS: Markdown image normalization and viewer source routing');

// Restoring the same asset paths must change the decoded-image cache key.
// Ordinary re-renders keep a stable URL; root, source and authority remain intact.
const helpers=source.slice(source.indexOf('function addKnowledgeViewerIdentity('),source.indexOf('function downloadFileEntry('));
const nodes=new Map(),spec={root:'context',source:'owned-source',path:'doc.md'},single={root:'workspace',path:'doc.md'};
const mediaContext={URLSearchParams,crypto:{randomUUID:()=> 'fresh-resource-revision'},state:{token:'fixture-token',file:spec},fileView:{spec:single},
 $:id=>{if(!nodes.has(id))nodes.set(id,{dispatchEvent(){assert.equal((id==='editor'?spec:single).resourceRevision,'restore-journal');}});return nodes.get(id);},
 Event:class{},editorAutoSave:{activate(){}},fileViewAutoSave:{activate(){}},loadFiles:()=>Promise.resolve(),toast(){},t:x=>x};
vm.createContext(mediaContext);vm.runInContext(helpers,mediaContext);
const before=mediaContext.fileRawUrl('image.png','context','owned-source',spec);
assert.equal(before,mediaContext.fileRawUrl('image.png','context','owned-source',spec));
for(const name of ['editorHistory','fileViewHistory']) {
 const line=source.split('\n').find(line=>line.startsWith('const '+name+' ='));
 const callback=vm.runInContext('('+line.split('onRestored:')[1].slice(0,-3)+')',mediaContext);
 callback({journal:'restore-journal',hash:'new-hash',content:'restored'});
}
const after=mediaContext.fileRawUrl('image.png','context','owned-source',spec);
assert.notEqual(after,before);const query=new URLSearchParams(after.split('?')[1]);
assert.equal(query.get('resourceRevision'),'restore-journal');assert.equal(query.get('source'),'owned-source');assert.equal(query.get('path'),'image.png');assert.equal(query.get('access_token'),'fixture-token');
assert.equal(after,mediaContext.fileRawUrl('image.png','context','owned-source',spec));
mediaContext.invalidateMarkdownResources(spec,'second-journal');
assert.notEqual(mediaContext.fileRawUrl('image.png','context','owned-source',spec),after);
console.log('PASS: both restore callbacks invalidate resource URLs before preview refresh; unchanged renders remain stable');
