const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/file-autosave.js','utf8');
const {sourceIncludes,sourceIndex}=require('./fixtures/starmap-source.cjs');
// Save failures expose an explicit retry. Conflict recovery must only reread;
// accepting merged content remains a separate, deliberate user action.
for(const marker of ["setRetry('save',context,{text:editor.value})","setRetry('latest',context,draft)","await flush(true)","compareRecovery(draft,gen,latest)","const mode=retryMode,context=retryContext,draft=retryDraft,gen=generation,key=identity(context);if(!current(gen,key))return","if(!current(gen,key)||blocked?.())return;setRetry('');status.textContent=t('服务器版本已读取，请比较后选择')","t(mode==='latest'?'读取服务器版本失败，可重试':'保存重试失败，可再次重试')"])assert.ok(sourceIncludes(source,marker),marker);
const body=source.slice(sourceIndex(source,'function applyRecovery('),sourceIndex(source,'function compareRecovery('));
const events=[];
const editor={value:'',scrollTop:0,scrollLeft:0,focus(options){events.push('focus');assert.equal(options.preventScroll,true);this.scrollTop=0;},setSelectionRange(start,end){this.start=start;this.end=end;this.scrollTop=0;},dispatchEvent(event){events.push(event.type);if(event.type==='input')this.scrollTop=0;}};
vm.runInNewContext(body+';applyRecovery({start:3,end:8,scrollTop:2048,scrollLeft:17},"local draft text");',{editor,recoveryBanner:null,suspended:true,Event:class{constructor(type){this.type=type;}}});
assert.equal(editor.value,'local draft text');assert.equal(editor.start,3);assert.equal(editor.end,8);assert.equal(editor.scrollTop,2048);assert.equal(editor.scrollLeft,17);assert.deepEqual(events,['focus','input','scroll']);
console.log('Draft recovery selection and scroll ordering PASS');
