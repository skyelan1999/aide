#!/usr/bin/env python3
"""Generate an offline fixture from production rendering functions and CSS."""
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[2]
web = root / 'internal/server/web'
source = (web / 'app.js').read_text()
def function(name):
    start = re.search(r'^function ' + name + r'\(', source, re.M).start()
    return source[start:source.index('\n}', start) + 2]
functions = '\n'.join(function(name) for name in [
    'newLiveBoundaryState', 'advanceLiveBoundary', 'updatePlainLiveTail',
    'renderLiveAnswer', 'fixCjkEmphasis', 'sanitizeHtml', 'renderMarkdown'])
css = '\n'.join((web / path).read_text() for path in [
    'themes/dark/tokens.css', 'themes/light/tokens.css', 'themes/green.css',
    'themes/comfort.css', 'style.css', 'macos.css', 'experience.css', 'sanctum.css'])
sample = '\n\n'.join([
    '## A consistent answer',
    'Streaming text and saved text share one reading surface. ' + 'LongPathWithoutAnyBreak/' * 12,
    '- Read the current state\n- Keep the output inside the answer frame',
    '```javascript\nconst result = "' + 'long-code-' * 50 + '";\nconsole.log(result);\n```',
    '| File | Description |\n| --- | --- |\n| report.md | ' + 'wide table content ' * 40 + ' |',
    '> A quoted result stays within the same frame.',
    'The answer is complete.'])
html = '''<!doctype html><html data-theme="dark" data-palette="blue"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Conversation output layout</title><style>'''+css+'''</style><style id="conversation-style">'''+((web/'conversation.css').read_text()+'\n'+(web/'workbench.css').read_text())+'''</style><style>
body{display:block;overflow:auto;height:auto;padding:24px}.fixture-tools{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px}.fixture-tools button{padding:8px 12px;border:1px solid var(--line);border-radius:8px}.fixture-viewport{width:920px;max-width:100%;border:1px solid var(--line);padding:20px}.fixture-viewport.narrow{width:390px;padding:12px}#conversation{padding:0;overflow:visible}.fixture-report{font:12px/1.7 monospace;white-space:pre-wrap;margin-bottom:16px}
</style><h2>Conversation output layout</h2><p>Production renderer and CSS; synthetic data only. No connection to 8097.</p><div class="fixture-tools"><button id="theme">Light / dark</button><button id="narrow">390px / desktop</button><button id="palette">Professional / classic / comfort</button><button id="stream">Stream answer</button><button id="finish">Complete answer</button><button id="verify">Verify layout</button></div><div class="fixture-report" id="report"></div><div class="fixture-viewport"><div id="conversation"><div id="timeline"><article class="run" data-run="fixture"><div class="user-message">Please show a consistent answer frame, including code and a wide table.</div><div class="run-meta"><span>Assistant</span><span>Layout preview</span></div><div class="stage-outputs"><section class="stage-output"><div class="chat-answer stage-answer md-body"></div></section></div><div class="msg-actions"><button class="msg-btn">Copy</button><button class="msg-btn">Retry</button></div></article></div></div></div><script>'''+(web/'vendor/marked.min.js').read_text()+'''</script><script>'''+((web/'markdown-code.js').read_text()+'\n'+(web/'staged-output.js').read_text())+'''</script><script>
const $=id=>document.getElementById(id),t=s=>s,el=(tag,cls,text)=>{const node=document.createElement(tag);node.className=cls||'';if(text!=null)node.textContent=text;return node;};
const BLOCKED_TAGS=new Set(['script','iframe','object','embed','style','link','meta']);
const liveActiveBytes={},state={live:{},liveStable:{},autoScroll:false,session:{runs:[{id:'fixture',steps:[{content:''}]}]}};
const updateJumpBtn=()=>{};
'''+functions+'''
const sample='''+json.dumps(sample)+''';
const answer=()=>document.querySelector('.chat-answer');
let timer;
function stream(text=sample){state.live.fixture=text;renderLiveAnswer('fixture');}
function complete(){clearInterval(timer);answer().innerHTML=renderMarkdown(sample);}
function geometry(){const a=answer(),r=a.getBoundingClientRect();return {width:r.width,height:r.height,overflow:a.scrollWidth>a.clientWidth+1||$('timeline').scrollWidth>$('timeline').clientWidth+1,blocks:[...a.querySelectorAll('p,h2,ul,pre,table,blockquote')].map(e=>Math.round((e.getBoundingClientRect().top-r.top)*100)/100)};}
function verify(){
 clearInterval(timer);state.liveStable={};stream();const live=geometry();complete();const done=geometry();
 const delta=Math.abs(live.height-done.height),same=JSON.stringify(live.blocks)===JSON.stringify(done.blocks);
 const pass=delta<1&&live.width===done.width&&!live.overflow&&!done.overflow&&same;
 $('report').textContent=(pass?'PASS':'FAIL')+' | live/final width '+live.width+'/'+done.width+'; height '+live.height+'/'+done.height+'; same block positions '+same+'; overflow '+(live.overflow||done.overflow)+' | '+document.documentElement.dataset.theme+' / '+document.documentElement.dataset.palette;
 document.documentElement.dataset.result=pass?'pass':'fail';
}
$('verify').onclick=verify;
$('theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark';verify();};
$('narrow').onclick=()=>{document.querySelector('.fixture-viewport').classList.toggle('narrow');verify();};
$('palette').onclick=()=>{const values=['blue','green','comfort'];document.documentElement.dataset.palette=values[(values.indexOf(document.documentElement.dataset.palette)+1)%3];verify();};
$('stream').onclick=()=>{clearInterval(timer);state.liveStable={};let count=0;timer=setInterval(()=>{count=Math.min(sample.length,count+95);stream(sample.slice(0,count));if(count===sample.length)clearInterval(timer);},55);};
$('finish').onclick=complete;verify();
</script></html>'''
path=root/'.agent-state/conversation-output-stability/preview.html'
path.parent.mkdir(parents=True,exist_ok=True)
path.write_text(html)
print(path)
