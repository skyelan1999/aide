'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function popupSource(){
 const app=fs.readFileSync('internal/server/web/app.js','utf8');
 const start=app.indexOf('  async function checkDuePopups() {'),end=app.indexOf('\n  button.onclick',start);
 assert.ok(start>=0&&end>start,'reminder popup product function found');return app.slice(start,end);
}
function fixture(options={}){
 const due=[{id:'owned-a',title:'Fixture A',priority:'high',dueAt:'2026-10-26'},{id:'owned-b',title:'Fixture B',priority:'normal',dueAt:'2026-10-26'},{id:'owned-c',title:'Fixture C',priority:'low',dueAt:'2026-10-26'}];
 const calls=[],seen=new Set(),failed=new Set(options.failed||[]),host={children:[],replaceChildren(){this.children=[];},append(row){this.children.push(row);}};
 const dialog={open:false,showModal(){if(options.showFailure)throw Error('owned-show-failure');this.open=true;}};
 let allowed=true;
 const context={popupBusy:false,encodeURIComponent,document:{visibilityState:'visible'},window:{aideAvatarFeedback(){if(options.feedbackFailure)throw Error('owned-avatar-failure');}},
  center:{messageUI:{allowReminderPopup:()=>allowed}},$:id=>id==='reminder-alert'?dialog:host,
  el:(tag,cls,text)=>({tag,cls,text,children:[],append(...items){this.children.push(...items);}}),t:(text,...args)=>text.replace(/\{(\d+)\}/g,(_,i)=>args[Number(i)]),fmt:value=>value,
  api:async path=>{
   calls.push({path,open:dialog.open,rows:host.children.length});
   if(path==='/reminders/due'){if(options.revokeAfterFetch)allowed=false;if(options.hideAfterFetch)context.document.visibilityState='hidden';return due.filter(item=>!seen.has(item.id));}
   const match=/^\/reminders\/([^/]+)\/popup-seen$/.exec(path);assert.ok(match,'unexpected API');const id=decodeURIComponent(match[1]);
   if(failed.has(id))throw Error('owned-marker-failure');seen.add(id);return {};
  }};
 vm.createContext(context);vm.runInContext(popupSource(),context);
 return {context,calls,seen,failed,host,dialog,options,run:()=>vm.runInContext('checkDuePopups()',context),dismiss(){dialog.open=false;context.popupBusy=false;}};
}
async function run(){
 const partial=fixture({failed:['owned-b']});await partial.run();
 assert.equal(partial.dialog.open,true,'all reminders must show even when one marker fails');assert.equal(partial.host.children.length,3);
 const markers=partial.calls.filter(call=>call.path.endsWith('/popup-seen'));assert.equal(markers.length,3,'one failure must not suppress later markers');
 assert.ok(markers.every(call=>call.open&&call.rows===3),'markers must follow successful complete popup presentation');
 assert.deepEqual([...partial.seen],['owned-a','owned-c']);
 const count=partial.calls.length;await partial.run();assert.equal(partial.calls.length,count,'open popup should prevent duplicate batches');
 partial.dismiss();partial.failed.clear();await partial.run();assert.equal(partial.host.children.length,1);assert.equal(partial.seen.size,3,'failed marker can retry after close');
 const show=fixture({showFailure:true});await show.run();assert.equal(show.seen.size,0,'failed dialog must not consume notifications');assert.equal(show.context.popupBusy,false);show.options.showFailure=false;await show.run();assert.equal(show.dialog.open,true);assert.equal(show.seen.size,3);
 const hidden=fixture({hideAfterFetch:true});await hidden.run();assert.equal(hidden.dialog.open,false);assert.equal(hidden.seen.size,0);
 const revoked=fixture({revokeAfterFetch:true});await revoked.run();assert.equal(revoked.dialog.open,false);assert.equal(revoked.seen.size,0);
 const feedback=fixture({feedbackFailure:true});await feedback.run();assert.equal(feedback.dialog.open,true);assert.equal(feedback.seen.size,3,'avatar callback cannot break notification acknowledgement');
 console.log('PASS popup presentation before acknowledgement, partial failure/retry, show failure, visibility/policy and independent feedback');
}
module.exports={popupSource};
if(require.main===module)run().catch(error=>{console.error(error);process.exitCode=1;});
