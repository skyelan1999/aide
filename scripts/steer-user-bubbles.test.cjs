const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('internal/server/web/app.js','utf8');
function fn(name, required=true){const start=source.search(new RegExp('^function '+name+'\\(', 'm'));if(start<0){assert.equal(required,false,name);return '';}return source.slice(start,source.indexOf('\n}',start)+2);}
class Element {
  constructor(tag='div',cls='',text=''){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.dataset={};this.scrollTop=0;this.scrollHeight=3000;this.clientHeight=700;this.classList={toggle(){},add(){},remove(){},contains(){return false;}};}
  append(...items){this.children.push(...items);}
  replaceChildren(...items){this.children=[...items];}
  querySelectorAll(){return [];}
  setAttribute(){}
  addEventListener(){}
  scrollTo(options){this.scrollTop=options.top;}
}
const nodes=new Map();const $=id=>{if(!nodes.has(id))nodes.set(id,new Element());return nodes.get(id);};
const state={session:null,live:{},runPhase:{},thinkingOpen:new Set(),historyLimit:30};let liveRenders=0;
const sandbox={state,$,el:(tag,cls,text)=>new Element(tag,cls,text),document:{body:new Element()},window:{},ignoredProposals:new Set(),statuses:{running:'运行中',completed:'已完成'},labels:{agent:'执行'},t:s=>s,renderMarkdown:s=>s,syncAssistantModeControls(){},syncComposerApproval(){},renderApprovalReviews(){},renderClarification(){},setSendMode(){},estimateContext(){},renderQueueBar(){},updateJumpBtn(){},scheduleLiveRender(){liveRenders++;},sessionRunRenderSignature:()=>'',appendCachedSessionRun:()=>false};
vm.createContext(sandbox);vm.runInContext(fs.readFileSync('internal/server/web/staged-output.js','utf8'),sandbox);for(const name of ['setMessageActionIcon','sessionTextRevision','sessionPluginPlanView','sessionRevision','renderSteerMessages','renderTaskPrompt','renderStepThinking','renderSession']){const code=fn(name,name!=='sessionTextRevision');if(code)vm.runInContext(code,sandbox);}
const run={id:'run',prompt:'原始任务',mode:'chat',status:'running',steps:[{name:'chat',status:'running',content:'长回答'.repeat(1000)}],steers:[{content:'请保留中文\n缩进 <script>bad()</script>',queued:false,at:'one'},{content:'稍后执行',queued:true,at:'two'},{content:'请保留中文\n缩进 <script>bad()</script>',queued:false,at:'three'}]};
state.session={id:'A',updated:'same',runs:[run],messages:[]};state.live.run='正在输出'.repeat(1000);
const revision=sandbox.sessionRevision(state.session);
sandbox.renderSession();
const box=()=> $('timeline').children[0];
const bubbles=()=>box().children.filter(n=>n.className.includes('steer-message'));
assert.equal(bubbles().length,2);assert.equal(bubbles()[0].className,'user-message steer-message');assert.equal(bubbles()[0].textContent,run.steers[0].content);assert.equal(bubbles()[0].innerHTML,undefined,'interjections are safe literal user text');
assert.ok(box().children.indexOf(bubbles()[0])>box().children.findIndex(n=>n.className.includes('chat-answer')),'long answer cannot bury the interjection at task start');
assert.equal(box().children.at(-1),bubbles()[1]);assert.equal(box().children[0].textContent,'原始任务');assert.equal(liveRenders,1,'stream restoration continues');
sandbox.renderSession();assert.equal(bubbles().length,2,'rerender never duplicates bubbles');
run.steers[1].queued=false;assert.notEqual(sandbox.sessionRevision(state.session),revision,'queue promotion invalidates snapshot with unchanged timestamps and counts');sandbox.renderSession();assert.equal(bubbles().length,3);
const beforeEdit=sandbox.sessionRevision(state.session);run.steers[1].content='立即执行';assert.notEqual(sandbox.sessionRevision(state.session),beforeEdit,'same-length edits are visible');
run.steps[0].outputSegments=[{id:'stage-a',round:0,content:'earlier conclusion'}];
sandbox.renderSession();assert.equal(box().children.filter(n=>n.className==='stage-outputs').length,1,'production renderer keeps completed stages beside live tail');
run.status='completed';sandbox.renderSession();assert.equal(bubbles().length,3,'completion preserves accepted input');
run.mode='workflow';run.steps=[{name:'agent',status:'completed',content:'完成'}];sandbox.renderSession();assert.equal(bubbles().length,3,'workflow shares bubble behavior');
state.session={id:'B',runs:[{id:'b',prompt:'会话B',mode:'chat',status:'completed',steps:[],steers:[]}],messages:[]};sandbox.renderSession();assert.equal(bubbles().length,0,'switching sessions does not leak prior inputs');
assert.doesNotMatch(fn('renderSession'),/steer-msg|steer-tag/);
console.log('interjections: actual renderer uses tail user bubbles; queue promotion, repeated text, snapshot edits, refresh, completion, workflow, streaming and session isolation passed');
