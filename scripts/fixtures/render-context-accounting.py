#!/usr/bin/env python3
"""Offline production context-card renderer; optional sanitized live task/preview."""
import argparse
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[2]
web = root / 'internal/server/web'
source = (web / 'app.js').read_text()
parser = argparse.ArgumentParser()
parser.add_argument('--task', type=Path)
parser.add_argument('--preview', type=Path)
args = parser.parse_args()

def function(name):
    start = re.search(r'^(?:async )?function ' + name + r'\(', source, re.M).start()
    line = source[start:source.index('\n', start)]
    return line if line.endswith('}') else source[start:source.index('\n}', start) + 2]

def context(value):
    if not value:
        return None
    result = {key: value.get(key, 0) for key in ['inputEstimate', 'outputReserve', 'contextWindow']}
    result['usage'] = {key: value.get('usage', {}).get(key, 0) for key in ['prompt', 'completion', 'total', 'estimated']}
    return result

task = {'parallelism': 10, 'usage': {'prompt': 180000, 'completion': 3000, 'total': 183000}, 'parallel': {
    'plannerContext': {'inputEstimate': 4000, 'contextWindow': 65536},
    'parts': [{'context': {'inputEstimate': 4200 + i, 'contextWindow': 65536}} for i in range(10)],
    'mainContext': {'inputEstimate': 7000, 'contextWindow': 65536, 'usage': {'prompt': 8200}},
}}
if args.task:
    raw = json.loads(args.task.read_text())
    progress = raw.get('parallel', {})
    task = {'parallelism': raw['parallelism'], 'topicContext':context(raw.get('topicContext')), 'usage': context({'usage': raw.get('usage', {})})['usage'], 'parallel': {
        'plannerContext': context(progress.get('plannerContext')),
        'mainContext': context(progress.get('mainContext')),
        'parts': [{'context': context(part.get('context'))} for part in progress.get('parts', [])],
    }}
preview = {'inputEstimate': 5000, 'outputReserve': 4096, 'totalEstimate': 9096, 'contextWindow': 65536,
           'breakdown': {'system': {'tokens': 3500}, 'historyAssistant': {'tokens': 1500}}}
if args.preview:
    preview = json.loads(args.preview.read_text())
    preview.pop('messages', None)
    preview.pop('tools', None)
css = '\n'.join((web / name).read_text() for name in ['themes/dark/tokens.css', 'themes/light/tokens.css',
                  'style.css', 'macos.css', 'experience.css', 'sanctum.css', 'conversation.css'])
functions = '\n'.join(function(name) for name in ['fmtTokens', 'ensureContextMeterControls', 'contextParts',
                                                 'renderParallelContextDetail', 'renderContextMeter'])
html = '''<!doctype html><html data-theme="dark"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Independent parallel context</title><style>''' + css + '''body{display:block;height:auto;overflow:auto;padding:24px}
.tools{display:flex;flex-wrap:wrap;gap:8px;margin:16px 0}.tools button{padding:8px;border:1px solid var(--line-mid);border-radius:8px}
.fixture{width:300px;max-width:100%}.context-card{width:100%;box-sizing:border-box}</style>
<h2>并行上下文组件验收</h2><p>实际生产渲染器；此页面无模型请求，无配置写入。</p>
<div class="tools"><button id="parallel">并行任务</button><button id="legacy">旧任务</button><button id="ordinary">切换普通会话</button><button id="theme">明暗</button></div>
<section class="fixture"><div id="context-card" class="context-card"><div class="context-head"><span>下次请求上下文（估算）</span><span id="context-stat"></span></div>
<div id="context-bar" class="context-bar context-segments"></div><div id="context-card-detail" class="context-card-detail"></div></div></section><script>
const $=id=>document.getElementById(id),el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text!=null)e.textContent=text;return e;};
const t=(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n]);
const activeModel=()=>({contextWindow:65536}),showCtxTooltip=()=>{},moveCtxTooltip=()=>{},hideCtxTooltip=()=>{},renderContextCompactionState=()=>{};
const state={contextMeterSeq:{value:0}},task=TASK,preview=PREVIEW;
''' + functions + '''
function show(kind){state.session=kind==='ordinary'?{id:'ordinary',runs:[]}:{id:'parallel',runs:[kind==='legacy'?{parallelism:5}:task],sessionUsage:task.usage};renderContextMeter(preview,false);}
$('parallel').onclick=()=>show('parallel');$('legacy').onclick=()=>show('legacy');$('ordinary').onclick=()=>show('ordinary');
$('theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark';};show('parallel');
</script></html>'''
html = html.replace('TASK', json.dumps(task, ensure_ascii=False).replace('<', '\\u003c'))
html = html.replace('PREVIEW', json.dumps(preview, ensure_ascii=False).replace('<', '\\u003c'))
dest = root / '.agent-state/parallel-context-accounting/preview.html'
dest.parent.mkdir(parents=True, exist_ok=True)
dest.write_text(html)
print(dest)
