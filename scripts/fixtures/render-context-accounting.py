#!/usr/bin/env python3
"""Render the production context card and dialog offline with synthetic or redacted accounting data."""
import argparse
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[2]
web = root / 'internal/server/web'
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--task', type=Path, help='optional task JSON (only accounting fields are used)')
parser.add_argument('--preview', type=Path, help='optional context-preview JSON (only budget totals are used)')
args = parser.parse_args()


def counters(data, *names):
    data = data or {}
    return {name: data[name] for name in names if type(data.get(name)) is int and data[name] >= 0}


def usage(data):
    result = counters(data, 'prompt', 'completion', 'total')
    if (data or {}).get('estimated') is True:
        result['estimated'] = True
    return result


def request_context(data):
    if not data:
        return None
    return {**counters(data, 'inputEstimate', 'outputReserve', 'contextWindow'), 'usage': usage(data.get('usage'))}


def lane(data):
    result = {'context': request_context(data.get('context')), 'usage': usage(data.get('usage'))}
    if data.get('status') in ('queued', 'running', 'completed', 'failed', 'cancelled', 'interrupted'):
        result['status'] = data['status']
    return result


def accounting_task(raw):
    task = {**counters(raw, 'parallelism'), 'usage': usage(raw.get('usage')),
            'topicContext': request_context(raw.get('topicContext'))}
    if anchor := raw.get('contextAnchor'):
        task['contextAnchor'] = {**counters(anchor, 'promptEstimate', 'contextWindow'), 'usage': usage(anchor.get('usage'))}
        if anchor.get('scope') == 'parallel_merge':
            task['contextAnchor']['scope'] = 'parallel_merge'
    if progress := raw.get('parallel'):
        task['parallel'] = {'plannerContext': request_context(progress.get('plannerContext')),
                            'mainContext': request_context(progress.get('mainContext')),
                            'plannerHistory': [request_context(item) for item in progress.get('plannerHistory') or []],
                            'parts': [lane(item) for item in progress.get('parts') or []]}
    if environments := raw.get('environments'):
        task['environments'] = {**counters(environments, 'capacity', 'active'),
                                'results': [lane(item) for item in environments.get('results') or []]}
    return task


task = {
    'parallelism': 10, 'usage': {'prompt': 180000, 'completion': 3000, 'total': 183000},
    'contextAnchor': {'scope': 'parallel_merge', 'promptEstimate': 7000, 'contextWindow': 65536,
                      'usage': {'prompt': 8200, 'completion': 450}},
    'parallel': {'plannerContext': {'inputEstimate': 4000, 'contextWindow': 65536}},
    'environments': {'capacity': 10, 'active': 0, 'results': [
        {'assignment': '示例任务 A', 'status': 'completed', 'context': {'inputEstimate': 4200, 'contextWindow': 65536}},
        {'assignment': '示例任务 B', 'status': 'completed', 'context': {'inputEstimate': 4300, 'contextWindow': 65536}},
    ]}
}
session_accounting = {
    'sessionUsage': task['usage'], 'compactedMessages': 12,
    'lastCompaction': {'folded': 12, 'kept': 6, 'beforeTokens': 12000, 'afterTokens': 4050},
}
if args.task:
    task = accounting_task(json.loads(args.task.read_text()))
    # A task export cannot establish the session's lifetime usage or saved compactions.
    session_accounting = {}
budget = {'inputEstimate': 5000, 'outputReserve': 4096, 'contextWindow': 65536}
if args.preview:
    budget = counters(json.loads(args.preview.read_text()), 'inputEstimate', 'outputReserve', 'contextWindow')
preview = {**budget, 'breakdown': {}}

index = (web / 'index.html').read_text()
app = (web / 'app.js').read_text()


def production_element(tag, element_id):
    opening = re.search(rf'<{tag}\b[^>]*\bid="{element_id}"[^>]*>', index)
    if opening:
        depth = 1
        for marker in re.finditer(rf'</?{tag}\b[^>]*>', index[opening.end():]):
            depth += -1 if marker[0].startswith('</') else 1
            if depth == 0:
                return index[opening.start():opening.end() + marker.end()]
    raise ValueError(f'Production element missing: #{element_id}')


def function(source, name):
    # tsc emits top-level declarations at column zero, including compact one-line helpers.
    match = re.search(rf'^function {name}\(.*?(?=\n(?![ \t}}])|\Z)', source, re.M | re.S)
    if not match:
        raise ValueError(f'Production function missing: {name}')
    return match[0]


css = '\n'.join((web / name).read_text() for name in [
    'themes/dark/tokens.css', 'themes/light/tokens.css', 'style.css', 'macos.css',
    'experience.css', 'sanctum.css', 'conversation.css', 'workbench.css',
])
core = (web / 'app-core.js').read_text()
helpers = '\n'.join(re.search(rf'^const {re.escape(name)} = .+;$', core, re.M)[0] for name in ('$', 't'))
renderer = '\n'.join(function(app, name) for name in [
    'fmtTokens', 'contextUsageTotal', 'formatUsageTokens', 'currentContextUsage', 'latestRequestContext',
    'renderParallelContextDetail', 'requestContextReadout', 'paintSavedCompaction',
    'paintContextUsage', 'renderContextMeter', 'contextRunDuration', 'renderContextRuntime',
    'contextCompactionForLane', 'paintContextCompactionRing', 'paintContextCompactionRings', 'contextRingButton', 'paintContextUsageRings', 'paintContextEnvironmentList',
    'sessionCompactionVisualState', 'renderSessionCompactionIndicators', 'renderContextCompactionState',
])
card = production_element('div', 'context-card')
dialog = production_element('dialog', 'context-usage-dialog')
fixture_data = json.dumps({'task': task, 'preview': preview, 'sessionAccounting': session_accounting}, ensure_ascii=False).replace('<', '\\u003c')
html = f'''<!doctype html><html lang="zh-CN" data-theme="dark"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:">
<title>上下文组件离线预览</title><style>{css}</style><style>
body.fixture-page {{display:block;height:auto;overflow:auto;padding:24px}}
.fixture-tools {{display:flex;flex-wrap:wrap;gap:8px;margin:16px 0}}
.fixture-tools button {{padding:6px 10px;border:1px solid var(--line);border-radius:6px}}
.fixture-tools [aria-pressed=true] {{color:var(--brand-text);border-color:currentColor}}
.fixture-preview {{width:260px;max-width:100%}}
</style></head><body class="fixture-page"><h1>上下文组件离线预览</h1>
<nav class="fixture-tools" aria-label="预览场景"><button id="parallel">并行任务</button><button id="legacy">旧任务</button>
<button id="ordinary">切换普通会话</button><button id="theme">明暗</button></nav>
<div class="fixture-preview"><div class="sidebar-footer">{card}</div></div>{dialog}
<script type="application/json" id="fixture-data">{fixture_data}</script><script>
{(web / 'model-token-chart.js').read_text()}
{helpers}
{function((web / 'app-sidebar.js').read_text(), 'el')}
{renderer}
</script>''' + '''
<script>
const {task, preview, sessionAccounting} = JSON.parse($('fixture-data').textContent);
const state = {contextMeterSeq: {value: 0}, contextCompactions: new Map()};
const scenarios = {
    parallel: {id: 'parallel', runs: [task], ...sessionAccounting},
    legacy: {id: 'legacy', runs: [{parallelism: 5}]},
    ordinary: {id: 'ordinary', runs: []}
};
function disableOfflineAction() {
    const action = $('context-compaction-action');
    action.disabled = true;
    action.title = '离线预览无法提交压缩';
}
function show(kind) {
    state.session = scenarios[kind];
    $('context-environments').open = false;
    renderContextMeter(preview, false);
    disableOfflineAction();
    for (const id of Object.keys(scenarios)) $(id).setAttribute('aria-pressed', String(id === kind));
}
for (const id of Object.keys(scenarios)) $(id).onclick = () => show(id);
$('context-toggle').onclick = () => {
    $('context-usage-dialog').showModal();
    paintContextUsage(state.contextMeter);
    disableOfflineAction();
};
$('theme').onclick = () => {
    const root = document.documentElement;
    root.dataset.theme = root.dataset.theme === 'dark' ? 'light' : 'dark';
};
show('parallel');
</script></body></html>'''
dest = root / '.agent-state/parallel-context-accounting/preview.html'
dest.parent.mkdir(parents=True, exist_ok=True)
dest.write_text(html)
print(dest)
