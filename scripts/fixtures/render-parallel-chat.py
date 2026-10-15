#!/usr/bin/env python3
"""Offline fixture for the production parallel selector and progress renderer."""
from pathlib import Path
import re
root=Path(__file__).resolve().parents[2]
web=root/'internal/server/web'
css='\n'.join((web/f).read_text() for f in ['themes/dark/tokens.css','themes/light/tokens.css','style.css','macos.css','experience.css','sanctum.css','conversation.css'])
index=(web/'index.html').read_text()
picker=re.search(r'<label class="parallel-picker">.*?</label>',index).group()
html='''<!doctype html><html data-theme="dark"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Parallel chat 1x-5x / 10x</title><style>'''+css+'''body{display:block;height:auto;overflow:auto;padding:28px}.fixture{width:840px;max-width:100%}.tools{display:flex;gap:10px;flex-wrap:wrap;margin-bottom:18px}.tools button{border:1px solid var(--line);padding:8px 12px;border-radius:8px}#report{font:12px/1.8 monospace;white-space:pre-wrap}.composer{padding:10px}.composer-toolbar{justify-content:flex-start}#timeline{margin-top:20px}</style><h2>Parallel chat</h2><p>Production selector and renderer. Synthetic progress only; no model calls.</p><div class="tools"><button id="theme">Light / dark</button><button id="narrow">Narrow / wide</button><button id="mode">Chat / workflow</button><button id="progress">Next stage</button></div><div id="report"></div><section class="fixture"><div class="composer"><textarea aria-label="Task" placeholder="Describe a task"></textarea><div class="composer-toolbar"><button class="strategy-button">Default model strategy</button>'''+picker+'''<div class="mode-switch"><button type="button">Chat</button><button type="button">AI workflow</button></div></div></div><div id="timeline"></div></section><script>'''+(web/'parallel-chat.js').read_text()+'''</script><script>
const $=id=>document.getElementById(id),t=(s,...args)=>s.replace(/\\{(\\d+)\\}/g,(_,n)=>args[n]),el=(tag,cls,text)=>{const node=document.createElement(tag);node.className=cls||'';if(text!=null)node.textContent=text;return node;};let mode='chat',phase=0;
const phases=['planning','running','merging','completed','fallback','interrupted'];
function render(){
 const limit=AideParallelChat.value(mode,false);$('report').textContent='selected='+$('chat-parallelism').value+'x; request parallelism='+limit+'; mode='+mode+'; disabled='+$('chat-parallelism').disabled;
 const run={id:'fixture',parallelism:Number($('chat-parallelism').value),parallel:{phase:phases[phase],parts:phase===0?[]:[{title:'Analyze requirements',status:phase<2?'running':'completed'},{title:'Check independent evidence',status:phase<2?'running':'completed'}]}};
 const panel=AideParallelChat.render(run,{el,t,open:new Set(),initial:true});$('timeline').replaceChildren();if(panel)$('timeline').append(panel);
 if(phase===3){const answer=el('div','chat-answer md-body');answer.textContent='One coherent final answer, reconciled from the independent analyses.';$('timeline').append(answer);}
}
AideParallelChat.install({select:$('chat-parallelism'),mode:()=>mode,assistant:()=>false,t});$('chat-parallelism').addEventListener('change',render);
$('theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark';};
$('narrow').onclick=()=>{const e=document.querySelector('.fixture');e.style.width=e.style.width==='390px'?'840px':'390px';};
$('mode').onclick=()=>{mode=mode==='chat'?'workflow':'chat';AideParallelChat.sync();render();};
$('progress').onclick=()=>{phase=(phase+1)%phases.length;render();};render();
</script></html>'''
p=root/'.agent-state/conversation-parallel/preview.html';p.parent.mkdir(parents=True,exist_ok=True);p.write_text(html);print(p)
