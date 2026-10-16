#!/usr/bin/env python3
"""Render the production thinking card with controlled tokens and poll replacement."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[2]
web = root / 'internal/server/web'
source = (web / 'app.js').read_text()
names = ['renderRunStatusInto', 'updateThinkingSummary', 'bindThinkingDisclosure',
         'runReasoningText', 'renderStepThinking', 'thinkAtBottom']
functions = '\n'.join(re.search(r'^function ' + name + r'\([\s\S]*?\n}', source, re.M).group()
                      for name in names)
css = '\n'.join((web / path).read_text() for path in
                ['themes/dark/tokens.css', 'themes/light/tokens.css', 'style.css', 'conversation.css'])
html = '''<!doctype html><html data-theme="light"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Thinking preview acceptance</title>
<style>''' + css + '''
body{display:block;padding:20px;height:auto;overflow:auto}main{max-width:800px;margin:auto}
nav{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px}.run{padding:15px;border:1px solid var(--line-faint)}
</style><main><h2>思考过程 · 流式交互验收</h2><p>生产组件，合成内容，不调用模型。</p>
<nav><button id="token">追加最新内容</button><button id="poll">轮询重建</button>
<button id="stream">持续流式更新</button><button id="finish">完成任务</button>
<button id="theme">切换明暗</button><button id="replace">替换保留内容</button></nav>
<div id="timeline"></div><p id="report"></p></main><script>
const el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text!=null)n.textContent=text;return n},t=s=>s;
const run={id:'fixture',mode:'chat',status:'running',steps:[{reasoning:'第一行：读取任务信息。\\n当前思考：准备核对最新结果。'}]};
const state={session:{runs:[run]},runPhase:{fixture:{phase:'reasoning',tools:[],startedAt:Date.now()}},liveReasoning:{},thinkingOpen:new Set()};
const phaseLabel=()=> '模型思考中',formatElapsed=()=>'',runWaitingForUser=()=>false;
''' + functions + '''
let n=0,timer;
function render(){const box=el('article','run');document.querySelector('#timeline').replaceChildren(box);
if(run.status==='running')renderRunStatusInto(box,run.id);else renderStepThinking(box,run,run.steps[0],0)}
function token(){run.steps[0].reasoning+='\\n当前思考 '+(++n)+'：核对新增结果，并继续后续分析。';const box=document.querySelector('.run');renderRunStatusInto(box,run.id);document.querySelector('#report').textContent='最新内容 '+n;}
document.querySelector('#token').onclick=token;document.querySelector('#poll').onclick=render;
document.querySelector('#stream').onclick=()=>{if(timer){clearInterval(timer);timer=null}else timer=setInterval(token,75)};
document.querySelector('#finish').onclick=()=>{clearInterval(timer);timer=null;run.status='completed';state.runPhase.fixture.done=true;render()};
document.querySelector('#theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='light'?'dark':'light'};
document.querySelector('#replace').onclick=()=>{run.steps[0].reasoning='保留记录已更新。\\n最新结果：完整正文已替换。';renderRunStatusInto(document.querySelector('.run'),run.id)};
render();</script></html>'''
path = root / '.agent-state/reasoning-live-preview/preview.html'
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(html)
print(path)
