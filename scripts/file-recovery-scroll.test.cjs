const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/file-autosave.js','utf8');
// Save failures expose an explicit retry. Conflict recovery must only reread;
// accepting merged content remains a separate, deliberate user action.
assert.ok(source.includes("setRetry('save',context,{text:editor.value})"));
assert.ok(source.includes("setRetry('latest',context,draft)"));
assert.ok(source.includes("await flush(true)"));
assert.ok(source.includes("compareRecovery(draft,gen,latest)"));
assert.ok(source.includes("const mode=retryMode,context=retryContext,draft=retryDraft,gen=generation,key=identity(context);if(!current(gen,key))return"));
assert.ok(source.includes("if(!current(gen,key)||blocked?.())return;setRetry('');status.textContent=t('服务器版本已读取，请比较后选择')"));
assert.ok(source.includes("t(mode==='latest'?'读取服务器版本失败，可重试':'保存重试失败，可再次重试')"));
const body=source.slice(source.indexOf('    function applyRecovery('),source.indexOf('    function compareRecovery('));
const events=[];
const editor={value:'',scrollTop:0,scrollLeft:0,focus(options){events.push('focus');assert.equal(options.preventScroll,true);this.scrollTop=0;},setSelectionRange(start,end){this.start=start;this.end=end;this.scrollTop=0;},dispatchEvent(event){events.push(event.type);if(event.type==='input')this.scrollTop=0;}};
vm.runInNewContext(body+';applyRecovery({start:3,end:8,scrollTop:2048,scrollLeft:17},"local draft text");',{editor,recoveryBanner:null,suspended:true,Event:class{constructor(type){this.type=type;}}});
assert.equal(editor.value,'local draft text');assert.equal(editor.start,3);assert.equal(editor.end,8);assert.equal(editor.scrollTop,2048);assert.equal(editor.scrollLeft,17);assert.deepEqual(events,['focus','input','scroll']);
console.log('Draft recovery selection and scroll ordering PASS');
