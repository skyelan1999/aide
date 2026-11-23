'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const {readAppSource,sourceAssignment,sourceIncludes}=require('./fixtures/app-source.cjs'),source=readAppSource(),sync=sourceAssignment(source,'window.aideAssistantLiveSync');
for(const enabled of [true,false]){let paints=0,starts=[];const context={window:{aideAssistantLive:{snapshot:()=>({enabled,paused:false}),start:microphone=>starts.push(microphone),resume(){}}},valid:()=>true,paint:()=>paints++};vm.runInNewContext(sync,context);context.window.aideAssistantLiveSync();assert.equal(paints,1,'entry must repaint even if background watch is already enabled');assert.ok(starts.every(v=>v===false),'entering assistant never opens microphone');}
assert.ok(sourceIncludes(source,"translate:t,valid:()=>valid()&&state.session?.kind==='assistant',"),'entry visibility follows assistant gates, not document background state');
console.log('PASS assistant entry sync paints without microphone activation');
