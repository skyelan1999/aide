#!/usr/bin/env python3
"""Offline production composer/pause controls; synthetic saves, no model calls."""
from pathlib import Path
import re
root=Path(__file__).resolve().parents[2];web=root/'internal/server/web';source=(web/'app.js').read_text()
def function(name):
 start=re.search(r'^function '+name+r'\(',source,re.M).start()
 return source[start:source.index('\n}',start)+2]
functions='\n'.join(function(n) for n in ['currentPauseRun','syncPauseControl','pauseRunById','setSendMode'])
index=(web/'index.html').read_text();controls=re.search(r'<div class="input-actions">(.*?)</div>',index).group()
css='\n'.join((web/f).read_text() for f in ['themes/dark/tokens.css','themes/light/tokens.css','style.css','macos.css','experience.css','sanctum.css','conversation.css'])
html='''<!doctype html><html data-theme="dark"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Task pause isolation</title><style>'''+css+'''body{display:block;height:auto;overflow:auto;padding:28px}.fixture{width:820px;max-width:100%}.tools{display:flex;gap:10px;flex-wrap:wrap;margin-bottom:14px}.tools button{border:1px solid var(--line);border-radius:8px;padding:8px 12px}#report{font:12px/1.8 monospace;white-space:pre-wrap}#timeline{min-height:130px;margin-bottom:20px}.composer{width:100%}.input-wrap{position:relative} .fixture .input-actions{display:flex;gap:8px}</style><h2>任务暂停 · 会话隔离</h2><p>实际输入区控件与处理函数，合成任务；不连接模型。</p><div class="tools"><button id="a">会话 A</button><button id="b">会话 B</button><button id="finish">完成暂停回执</button><button id="reset">重置任务</button><button id="theme">明暗主题</button><button id="narrow">窄屏</button></div><section class="fixture"><div id="report"></div><div id="timeline"><article class="run"><div class="user-message">分析当前任务</div><div class="run-meta"><span>AIDE ASSISTANT</span><span>运行中</span></div><div class="step-content"><p>回复区域展示内容，暂停位于下方输入区。</p></div></article></div><form class="composer"><div class="input-wrap"><textarea id="prompt" rows="2" aria-label="任务描述" placeholder="插话或继续输入…"></textarea>'''+controls+'''</div></form><p id="notice"></p></section><script>
const $=id=>document.getElementById(id),t=s=>s;
const sessions={a:{id:'a',runs:[{id:'run-a',status:'running',canResume:true}]},b:{id:'b',runs:[{id:'run-b',status:'running',canResume:true}]}};
const state={session:sessions.a,config:{workspaceId:'fixture'}};const recoveryScope=()=>state.config.workspaceId;let complete,requests=0;
const api=()=>{requests++;report();return new Promise(resolve=>{complete=resolve})},toast=s=>{$('notice').textContent=s;},schedulePoll=()=>{};
'''+functions+'''
function report(){$('report').textContent='当前会话 '+state.session.id+' | A: '+(sessions.a.runs[0].pauseRequested?'正在暂停':'运行中')+' | B: '+(sessions.b.runs[0].pauseRequested?'正在暂停':'运行中')+' | 请求数 '+requests;}
function render(){setSendMode(true);report();}
$('pause-current-run').onclick=async()=>{const run=currentPauseRun();if(run)await pauseRunById(run.id,state.session.id);render();};
$('send').type='button';for(const id of ['a','b'])$(id).onclick=()=>{state.session=sessions[id];render();};
$('finish').onclick=()=>{complete?.({});};$('reset').onclick=()=>{for(const s of Object.values(sessions))s.runs[0].pauseRequested=false;render();};
$('theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark';};$('narrow').onclick=()=>{document.querySelector('.fixture').style.maxWidth='390px';};render();
</script></html>'''
p=root/'.agent-state/pause-session-isolation/preview.html';p.parent.mkdir(parents=True,exist_ok=True);p.write_text(html);print(p)
