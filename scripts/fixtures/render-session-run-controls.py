#!/usr/bin/env python3
"""Offline control and conversation preview using production functions and CSS."""
import json
import re
from pathlib import Path
root=Path(__file__).resolve().parents[2]
web=root/'internal/server/web'
source=(web/'app.js').read_text()
def fn(name):
    start=re.search(r'^(?:async )?function '+name+r'\(',source,re.M).start()
    eol=source.index('\n',start)
    return source[start:eol] if source[eol-1]=='}' else source[start:source.index('\n}',start)+2]
names=['currentPauseRun','syncPauseControl','composerControlRun','composerHasDraft','setSendMode','pauseRunById','stopRunById','renderSession','renderSteerMessages','renderStepThinking','runReasoningText','renderRunStatusInto','updateThinkingSummary','renderToolRow','phaseLabel','formatElapsed','thinkAtBottom','runWaitingForUser','renderLiveAnswer','newLiveBoundaryState','advanceLiveBoundary','updatePlainLiveTail','fixCjkEmphasis','sanitizeHtml','renderMarkdown']
css='\n'.join((web/p).read_text() for p in ['themes/dark/tokens.css','themes/light/tokens.css','themes/green.css','themes/comfort.css','style.css','macos.css','experience.css','sanctum.css','conversation.css'])
index=(web/'index.html').read_text();start=index.index('<div id="run-controls"');controls=index[start:index.index('<div class="composer-toolbar">',start)]+'</form>'
html='''<!doctype html><html data-theme="dark" data-palette="blue"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Session run controls acceptance</title><style>'''+css+'''</style><style>
body{display:block;height:auto;overflow:auto;padding:16px}.preview{width:850px;max-width:100%;margin:auto}.preview.narrow{width:390px}.fixture-tools{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px}#conversation{height:350px;min-height:0;padding:0 0 12px;overflow:auto}.composer-wrap{position:relative;padding:0;width:100%}.composer{width:100%}#run-control-status{flex:1}#report{font:12px/1.6 monospace;white-space:pre-wrap}.fixture-tools button{padding:7px 10px}
</style><main class="preview"><h2>会话运行与流式输出</h2><p>离线生产组件；模拟 API，不调用模型或操作真实会话。</p><div class="fixture-tools"><button id="demo-run">运行</button><button id="demo-stream">流式输出</button><button id="demo-wait">等待审批</button><button id="demo-complete">完成</button><button id="demo-empty">切换空会话</button><button id="demo-theme">明暗主题</button><button id="demo-palette">专业/经典/舒适</button><button id="demo-width">窄屏/桌面</button><button id="demo-queue">插话/排队</button></div><div id="report" role="status"></div><span id="session-title"></span><div id="welcome" hidden></div><div id="conversation"><div id="timeline"></div></div><div class="composer-wrap">'''+controls+'''</div></main><script>'''+(web/'vendor/marked.min.js').read_text()+'''</script><script>'''+(web/'markdown-code.js').read_text()+'''</script><script>'''+(web/'staged-output.js').read_text()+'''</script><script>
const $=id=>document.getElementById(id),t=(s,...args)=>s.replace(/\\{(\\d+)\\}/g,(_,i)=>args[i]??''),el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text!=null)n.textContent=text;return n};
const state={session:null,live:{},liveStable:{},liveRound:{},liveTool:{},liveReasoning:{},runPhase:{},thinkingOpen:new Set(),attachments:[],historyScroll:false,autoScroll:false};
const stopRequests=new Set(),resumeRequests=new Set(),ignoredProposals=new Set(),liveActiveBytes={},sessionSeq={value:1},lockScreen={locked:false};
const BLOCKED_TAGS=new Set(['script','iframe','object','embed','style','link','meta']);
const statuses={running:'运行中',completed:'已完成',paused:'已暂停',cancelled:'已停止',interrupted:'已中断',awaiting_clarification:'等待回答'};
const recoveryScope=()=> 'offline-fixture';const labels={};
function syncAssistantModeControls(){}function syncComposerApproval(){}function renderApprovalReviews(){}function renderClarification(){}function renderQueueBar(){}function estimateContext(){}function updateJumpBtn(){}function toast(s){$('report').textContent=s}function refreshSessionSoon(){renderSession()}function schedulePoll(){renderSession()}function retryRunById(){demo('running')}function resumeRunById(){demo('running')}function sessionRunRenderSignature(){return ''}function appendCachedSessionRun(){return false}function scheduleLiveRender(id){renderLiveAnswer(id)}
function action(fn){return (...args)=>Promise.resolve(fn(...args)).catch(e=>toast(e.message))}
async function api(path){const run=state.session.runs[0];if(path.endsWith('/cancel')){run.status='cancelled';run.error='任务已停止';run.steps[0].content=state.live[run.id]||run.steps[0].content;state.live={};state.runPhase={}}else if(path.endsWith('/pause')){run.status='paused';run.steps[0].content=state.live[run.id]||run.steps[0].content;state.live={};state.runPhase={}}return {ok:true}}
'''+ '\n'.join(fn(name) for name in names)+'''
const sample='## 分段输出保持可见\\n\\n先检查当前任务的状态，再保留已经输出的结论。\\n\\n```javascript\\nconst result = await runTask();\\nconsole.log(result);\\n```\\n\\n- 插话用于调整当前工作\\n- 排队用于后续需求\\n\\n这段正文在暂停或停止之后仍然保留。';
let streamTimer;
function demo(status){clearInterval(streamTimer);state.session={id:'fixture',runs:[{id:'r',mode:'chat',prompt:'请解释任务运行过程，并保留已完成的结论。',model:'Fixture',status,canResume:true,steps:[{name:'chat',status:status==='running'?'running':'completed',content:sample,reasoning:'读取状态并确认会话归属，然后组织输出。'}],steers:[{content:'请保留现有结论，继续补充说明。',queued:false}]}]};state.live={};state.runPhase={};if(status==='running'){state.live.r=sample;state.runPhase.r={tools:[{callId:'read',tool:'read_file',args:'README.md',status:'running',startedAt:Date.now()}],phase:'generating',startedAt:Date.now()}}renderSession();$('report').textContent='生产渲染器 · '+status;}
$('task-form').onsubmit=e=>{e.preventDefault();if(!$('prompt').value.trim())return;state.session.runs[0].steers.push({content:$('prompt').value,queued:!!state.queueMode});$('prompt').value='';renderSession();toast(state.queueMode?'已排队':'已发送插话')};
$('send').onclick=e=>{if($('send').dataset.action==='stop'){e.preventDefault();action(()=>stopRunById('r'))()}};
$('prompt').oninput=()=>setSendMode(state.busy);
$('stop-active-run').onclick=action(()=>stopRunById('r'));
$('pause-current-run').onclick=action(()=>{ $('run-control-more').open=false;return pauseRunById('r')});
$('resume-active-run').onclick=()=>demo('running');
$('demo-stream').onclick=()=>{demo('running');const run=state.session.runs[0];run.steps[0].content='';state.live.r='';renderSession();let n=0;streamTimer=setInterval(()=>{n=Math.min(sample.length,n+12);state.live.r=sample.slice(0,n);renderLiveAnswer('r');if(n===sample.length){clearInterval(streamTimer);run.steps[0].content=sample;run.status='completed';state.live={};state.runPhase={};renderSession();$('report').textContent='流式完成，持久化正文接管';}},100)};
$('demo-run').onclick=()=>demo('running');$('demo-wait').onclick=()=>demo('awaiting_clarification');$('demo-complete').onclick=()=>demo('completed');
$('demo-empty').onclick=()=>{state.session={id:'other',runs:[]};renderSession();toast('空会话：没有前一会话的任务控制或输出')};
$('demo-theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark'};
$('demo-palette').onclick=()=>{const p=['blue','green','comfort'];document.documentElement.dataset.palette=p[(p.indexOf(document.documentElement.dataset.palette)+1)%p.length]};
$('demo-width').onclick=()=>document.querySelector('.preview').classList.toggle('narrow');$('demo-queue').onclick=()=>{state.queueMode=!state.queueMode;setSendMode(state.busy)};
demo('running');
</script></html>'''
path=root/'.agent-state/session-run-interaction/preview.html';path.parent.mkdir(parents=True,exist_ok=True);path.write_text(html);print(path)
