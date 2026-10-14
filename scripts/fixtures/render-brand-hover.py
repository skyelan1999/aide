#!/usr/bin/env python3
"""Offline brand-hover fixture using the complete production style cascade."""
from pathlib import Path
import re
root=Path(__file__).resolve().parents[2]
web=root/'internal/server/web'
css='\n'.join((web/f).read_text() for f in ['themes/dark/tokens.css','themes/light/tokens.css','style.css','macos.css','experience.css','sanctum.css','conversation.css'])
brand=re.search(r'<button[^>]*id="brand-button".*?</button>',(web/'index.html').read_text()).group()
html='''<!doctype html><html data-theme="dark" data-motion="full"><meta charset="utf-8"><title>Brand hover restore</title><style>'''+css+'''body{display:block;padding:36px;height:auto}.preview{width:250px;padding:20px;border:1px solid var(--line);margin-top:24px}.tools{display:flex;gap:12px}.tools button{padding:10px;border:1px solid var(--line)}#readout{white-space:pre-wrap;font:13px/1.8 monospace}</style><h2>Brand hover restore</h2><p>Production brand and full CSS cascade. Synthetic settings action only.</p><div class="tools"><button id="theme">Light / dark</button><button id="motion">Full / reduced motion</button></div><div class="preview">'''+brand+'''</div><p id="readout"></p><script>
const brand=document.getElementById('brand-button'),icon=brand.querySelector('.brand-icon'),out=document.getElementById('readout');let clicks=0;
function report(){out.textContent='motion='+document.documentElement.dataset.motion+'; hover='+brand.matches(':hover')+'; keyboard focus='+brand.matches(':focus-visible')+'; transform='+getComputedStyle(icon).transform+'; settings clicks='+clicks;}
brand.addEventListener('click',()=>{clicks++;report();});
for(const event of ['pointerenter','pointerleave','focus','blur'])brand.addEventListener(event,report);
icon.addEventListener('transitionend',report);
document.getElementById('theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark';report();};
document.getElementById('motion').onclick=()=>{document.documentElement.dataset.motion=document.documentElement.dataset.motion==='full'?'reduced':'full';report();};
report();
</script></html>'''
p=root/'.agent-state/brand-hover-restore/preview.html';p.parent.mkdir(parents=True,exist_ok=True);p.write_text(html);print(p)
