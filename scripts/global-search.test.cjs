const assert = require('node:assert/strict');
const vm = require('node:vm');
const {readAppSource,sourceFunction,sourceMatch}=require('./fixtures/app-source.cjs');
const source=readAppSource();
function fn(name){return sourceFunction(source,name);}

function setup(){
  const nodes=new Map();
  const document={activeElement:null};
  class Node {
    constructor(tag='div',className='',text=''){
      this.tag=tag;this.className=className;this.textContent=text;this.children=[];this.value='';this.type='';
      const values=new Set(className.split(/\s+/).filter(Boolean));
      this.classList={add(...names){names.forEach(name=>values.add(name));},remove(...names){names.forEach(name=>values.delete(name));},contains(name){return values.has(name);}};
    }
    append(...items){this.children.push(...items);}
    replaceChildren(...items){this.children=items;}
    querySelectorAll(selector){return selector==='.search-result'?this.children.filter(item=>item.classList.contains('search-result')):[];}
    focus(){document.activeElement=this;}
    click(){return this.onclick?.();}
  }
  const $=id=>{if(!nodes.has(id))nodes.set(id,new Node());return nodes.get(id);};
  const input=$('global-search');document.activeElement=input;
  let timer=null;
  const selected=[];
  const calls=[];
  const sandbox={
    document,window:{aideExperience:{reopen(){}}},console,
    $,el:(tag,className,text)=>new Node(tag,className,text),
    t:text=>text,action:fn=>fn,leaveUISurface:(node,callback)=>callback(),
    setTimeout:callback=>{timer=callback;return 1;},clearTimeout:()=>{timer=null;},
    api:async path=>{calls.push(path);return {results:[
      {sessionId:'one',number:285,title:'百万字长篇小说创作',snippet:'百万字长篇小说创作',archived:false},
      {sessionId:'two',number:257,title:'另一会话',snippet:'正文片段',archived:true},
    ]};},
    loadSessions:async()=>{},selectSession:async id=>{selected.push(id);},
  };
  vm.createContext(sandbox);
  vm.runInContext('let searchTimer=null,searchSeq=0,searchResultsWanted=false;',sandbox);
  for(const name of ['closeGlobalSearch','searchMayShow','renderGlobalSearchResults','scheduleGlobalSearch','handleGlobalSearchKeydown'])vm.runInContext(fn(name),sandbox);
  return {sandbox,nodes,document,selected,calls,runTimer:async()=>{const callback=timer;timer=null;assert.equal(typeof callback,'function');await callback();}};
}

(async()=>{
  const {sandbox,nodes,document,selected,calls,runTimer}=setup();
  const input=nodes.get('global-search');
  input.value='百万字';
  sandbox.scheduleGlobalSearch();
  const host=nodes.get('search-results');
  assert.equal(host.children[0].textContent,'加载中…','input immediately exposes search progress');
  await runTimer();
  assert.match(calls[0],/^\/search\?meta=1&q=/,'title metadata search runs first');
  assert.match(calls[1],/^\/search\?q=/,'full-text search follows in the background');
  const rows=host.querySelectorAll('.search-result');
  assert.equal(rows.length,2);
  assert.equal(rows[0].children.length,2,'unarchived rows contain only title and snippet');
  assert.equal(rows[0].children.some(item=>item===null||item?.textContent==='null'),false,'null never becomes visible result text');
  assert.equal(rows[1].children.length,3,'archived rows retain their badge');
  assert.equal(rows[1].children[1].textContent,'已归档');

  const key=key=>({key,isComposing:false,preventDefault(){this.prevented=true;}});
  sandbox.handleGlobalSearchKeydown(key('ArrowDown'));
  assert.equal(document.activeElement,rows[0],'ArrowDown focuses the first result');
  sandbox.handleGlobalSearchKeydown(key('ArrowDown'));
  assert.equal(document.activeElement,rows[1],'ArrowDown moves to the next result');
  sandbox.handleGlobalSearchKeydown(key('ArrowUp'));
  assert.equal(document.activeElement,rows[0],'ArrowUp moves back');
  document.activeElement=input;
  sandbox.handleGlobalSearchKeydown(key('Enter'));
  await new Promise(setImmediate);
  assert.deepEqual(selected,['one'],'Enter opens the first result from the input');
  assert.equal(input.value,'');
  assert.equal(host.classList.contains('hidden'),true);
  sourceMatch(source,/\$\('global-search'\)\.closest\('\.global-search'\)\.addEventListener\('keydown', handleGlobalSearchKeydown\)/);
  console.log('global search tests passed');
})().catch(error=>{console.error(error);process.exitCode=1;});
