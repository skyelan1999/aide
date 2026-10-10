const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/file-autosave.js','utf8');
const body=source.slice(source.indexOf('    function applyRecovery('),source.indexOf('    function compareRecovery('));
const events=[];
const editor={value:'',scrollTop:0,scrollLeft:0,focus(options){events.push('focus');assert.equal(options.preventScroll,true);this.scrollTop=0;},setSelectionRange(start,end){this.start=start;this.end=end;this.scrollTop=0;},dispatchEvent(event){events.push(event.type);if(event.type==='input')this.scrollTop=0;}};
vm.runInNewContext(body+';applyRecovery({start:3,end:8,scrollTop:2048,scrollLeft:17},"local draft text");',{editor,recoveryBanner:null,suspended:true,Event:class{constructor(type){this.type=type;}}});
assert.equal(editor.value,'local draft text');assert.equal(editor.start,3);assert.equal(editor.end,8);assert.equal(editor.scrollTop,2048);assert.equal(editor.scrollLeft,17);assert.deepEqual(events,['focus','input','scroll']);
console.log('Draft recovery selection and scroll ordering PASS');
