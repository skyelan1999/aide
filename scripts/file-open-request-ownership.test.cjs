const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(process.argv[2]||'internal/server/web/app.js','utf8');
const start=source.includes('let fileOpenRequest=0;')?source.indexOf('let fileOpenRequest=0;'):source.indexOf('async function openFile(path) {');
const code=source.slice(start,source.indexOf('function sourceIsRW()',start));
function fixture(){
  const pending=[],shown=[],scenes=[],state={root:'workspace',source:'',config:{workspaceId:'A'},workspaceId:'A',file:null,token:'local-test'};
  const dialog={open:false,addEventListener(type,fn){this[type]=fn;}};
  const context={state,lockScreen:{locked:false},chatRecoveryEpoch:0,recoveryScope:()=>state.config.workspaceId,$:()=>dialog,
    api:query=>new Promise((resolve,reject)=>pending.push({query,resolve,reject})),showEditor:()=>shown.push({...state.file}),persistWorkspaceScene:async scene=>scenes.push(scene)};
  for(const type of ['Image','Stl','Step','Pdf','CadDrawing','Docx','Xlsx','Sqlite','Zip'])context['is'+type+'Path']=path=>type==='Image'&&path.endsWith('.png');
  context.effectiveFileTypePath=require('../internal/server/web/file-types.js').effectiveTypePath;
  vm.createContext(context);vm.runInContext(source.split('\n').find(line=>line.startsWith('function isPresentationPath(')),context);vm.runInContext(code,context);
  const closeHook=source.split('\n').find(line=>line.startsWith("$('editor-dialog').addEventListener('close'"));
  if(closeHook)vm.runInContext(closeHook,context);
  return {context,state,pending,shown,scenes,dialog,open:path=>context.openFile(path)};
}
const result=content=>({content,hash:content,workspaceId:'A'});
async function gridResponseOwnership(){
  const pending=[],nodes=[];
  function element(tag,cls='',text=''){
    const e={tag,className:cls,textContent:text,children:[],value:'',classList:{toggle(){}},setAttribute(){},addEventListener(){},
      append(...items){this.children.push(...items);},replaceChildren(...items){this.children=items;this.textContent='';}};
    Object.defineProperty(e,'options',{get(){return this.children;}});nodes.push(e);return e;
  }
  const context=vm.createContext({document:{createElement:element},el:element,t:x=>x,URLSearchParams,AbortController,
    bindViewerTeardown:(host,fn)=>{host.cleanup=fn;},addKnowledgeViewerIdentity:()=>false,isBackupPath:()=>false,
    api:query=>new Promise((resolve,reject)=>pending.push({query,resolve,reject}))});
  const begin=source.indexOf('async function setupXlsxGrid(');
  vm.runInContext(source.slice(begin,source.indexOf('function isDocPath(',begin)),context);
  const page=value=>({sheets:['A','B'],sheet:'A',hash:'H',workspaceId:'W',maxRow:1,maxCol:1,rows:[[{ref:'A1',value}]]});
  const host=element('div'),ready=context.setupXlsxGrid(host,'book.xlsx','workspace','');
  pending[0].resolve(page('initial'));await ready;
  const select=nodes.find(e=>e.tag==='select'),grid=nodes.find(e=>e.className==='xlsx-grid');
  select.value='B';select.onchange();select.value='A';select.onchange();
  pending[2].resolve(page('latest'));await new Promise(setImmediate);
  const latestTable=grid.children[0];
  pending[1].reject(new Error('obsolete sheet failure'));await new Promise(setImmediate);
  assert.equal(grid.textContent,'','late failed sheet must not overwrite the latest grid');assert.equal(grid.children[0],latestTable);
  select.value='B';select.onchange();pending[3].reject(new Error('current sheet failure'));await new Promise(setImmediate);
  assert.equal(grid.textContent,'current sheet failure','current sheet errors must remain visible');
  select.value='A';select.onchange();host.cleanup();pending[4].reject(new Error('closed viewer failure'));await new Promise(setImmediate);
  assert.equal(grid.textContent,'current sheet failure','closed grid must ignore failed reads');
}
(async()=>{
  const a=fixture(),old=a.open('old.md'),fresh=a.open('new.md');a.pending[1].resolve(result('NEW'));await fresh;a.pending[0].resolve(result('OLD'));await old;
  assert.equal(a.state.file.path,'new.md','late old response must not replace latest selected file');assert.equal(a.shown.length,1);assert.equal(a.scenes.length,1);
  const b=fixture();b.state.root='context';b.state.source='source-A';const read=b.open('same.md');b.state.source='source-B';b.pending[0].resolve(result('A content'));await read;assert.equal(b.shown.length,0,'source A response must not be attributed to B');
  const c=fixture(),readC=c.open('same.md');c.state.config.workspaceId='B';c.pending[0].resolve(result('A content'));await readC;assert.equal(c.shown.length,0,'old workspace response must be discarded');
  const d=fixture(),readD=d.open('old.md');await d.open('picture.png');d.pending[0].resolve(result('OLD'));await readD;assert.equal(d.state.file.path,'picture.png','visual viewer selection must supersede pending text read');
  const e=fixture(),readE=e.open('old.md');e.context.chatRecoveryEpoch++;e.pending[0].resolve(result('OLD'));await readE;assert.equal(e.shown.length,0,'lock/unlock recovery epoch must invalidate pending reads');
  const f=fixture(),readF=f.open('old.md');f.state.file={path:'created.md',fresh:true};f.pending[0].resolve(result('OLD'));await readF;assert.equal(f.state.file.path,'created.md','a newly created file must retain editor ownership');
  const g=fixture(),readG=g.open('old.md');g.context.lockScreen.locked=true;g.pending[0].resolve(result('OLD'));await readG;assert.equal(g.shown.length,0,'locked UI must not reopen editor');
  const h=fixture(),readH=h.open('same.md');h.pending[0].reject(new Error('offline'));await assert.rejects(readH,/offline/,'current failures must remain visible');
  const i=fixture(),readI=i.open('old.md');await i.open('picture.png');i.pending[0].reject(new Error('obsolete'));await readI;assert.equal(i.state.file.path,'picture.png','obsolete error must not affect newer view');
  const j=fixture(),readJ=j.open('normal.md');j.pending[0].resolve(result('NORMAL'));await readJ;assert.equal(j.shown[0].content,'NORMAL');assert.equal(j.scenes[0].openFile.path,'normal.md');
  const k=fixture(),readK=k.open('old.md');k.dialog.close();k.pending[0].resolve(result('OLD'));await readK;assert.equal(k.shown.length,0,'closing the editor must invalidate an outstanding read');
  const l=fixture();l.dialog.open=true;const readL=l.open('restored.md');l.pending[0].resolve(result('RESTORED'));await readL;assert.equal(l.shown.length,1,'initialization during login submission must still restore the scene');
  const m=fixture(),readM=m.open('old.md');m.dialog.open=true;m.pending[0].resolve(result('OLD'));await readM;assert.equal(m.shown.length,0,'new authentication dialog must invalidate an old response');
  const n=fixture(),readN=n.open('old.md');n.state.token='different';n.pending[0].resolve(result('OLD'));await readN;assert.equal(n.shown.length,0,'token changes must invalidate an old response');
  for(const path of ['deck.ppt','deck.pptx','deck.pptx.v0.10.bak']){const view=fixture();await view.open(path);assert.equal(view.pending.length,0,'presentation must bypass text API');assert.equal(view.shown[0].path,path);}
  await gridResponseOwnership();
  console.log('File open ownership PASS: latest selection, source/workspace isolation, visual/new-file replacement, lock epoch, current/stale errors, normal scene persistence');
})().catch(e=>{console.error(e);process.exitCode=1;});
