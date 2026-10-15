#!/usr/bin/env python3
"""Preview production workbench markup/styles/settings with synthetic local data."""
from pathlib import Path
import argparse,re,http.server
root=Path(__file__).resolve().parents[2];web=root/'internal/server/web'
s=(web/'index.html').read_text()
s=re.sub(r'<script\b[^>]*src="([^"]+)"[^>]*></script>',lambda m:m[0] if m[1].split('?')[0] in ['/settings-init.js','/workbench-background.js'] else '',s)
source=(web/'app.js').read_text();a=source.index('function renderSegmentedControl(');b=source.index('\nfunction renderCheckboxControl',a);segmented=source[a:b]
harness='''<script>
const $=id=>document.getElementById(id),t=s=>s;
function el(tag,cls,text){const node=document.createElement(tag);node.className=cls||'';if(text!=null)node.textContent=text;return node}
const settingsPanel={refreshers:[]};
'''+segmented+'''
document.addEventListener('DOMContentLoaded',async()=>{
 for(const dialog of document.querySelectorAll('dialog[open]'))dialog.close();
 $('lock-screen')?.setAttribute('hidden','');
 $('sessions').innerHTML='<button class="session-item active">工作台界面打磨</button><button class="session-item">知识图谱与文件预览</button><button class="session-item">ROS 2 架构说明</button>';
 $('files').innerHTML=['docs','internal','scripts','README.md','compose.yaml','start.command'].map((name,i)=>'<button class="file-item"><span class="file-icon">'+(i<3?'▱':'◇')+'</span><span class="file-name">'+name+'</span></button>').join('');
 $('connection').textContent='组件预览 · 无模型调用';
 const tools=el('div','preview-tools');
 tools.style.cssText='position:fixed;bottom:8px;right:8px;z-index:100;display:flex;gap:4px;background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:4px';
 const open=el('button','quiet','外观预览');open.onclick=()=>$('settings-sheet').classList.toggle('open');
 const content=el('button','quiet','欢迎/输出');content.onclick=()=>{
  $('welcome').hidden=!$('welcome').hidden;
  $('welcome').style.display=$('welcome').hidden?'none':'';
  $('timeline').innerHTML=$('welcome').hidden?'<article class="run"><div class="user-message">请统一工作台样式，并保留清楚的阅读层级。</div><div class="thinking-block"><summary>已思考 · 1.2 秒</summary></div><div class="chat-answer"><h2>工作台布局</h2><p>导航、内容与工具区域采用一致的间距。较长的中文与 mixed content 仍然保持清楚的段落层级。</p><pre><code>const background = { mode: "aurora", paused: false };\\nconsole.log(background.mode);</code></pre><table><thead><tr><th>区域</th><th>改进</th></tr></thead><tbody><tr><td>会话</td><td>正文与流式内容统一</td></tr><tr><td>工具栏</td><td>固定图标比例与按钮高度</td></tr></tbody></table><p>已经完成的结论保留在当前会话中。</p></div></article>':'';
 };
 tools.append(open,content);document.body.append(tools);
 if(new URLSearchParams(location.search).get('scene')==='answer')content.click();
 $('settings-sheet-close').onclick=()=>$('settings-sheet').classList.remove('open');
 const schema=await fetch('/settings-schema.json').then(r=>r.json());
 $('settings-nav').innerHTML='<button class="settings-nav-item active">外观</button><button class="settings-nav-item">语言</button><button class="settings-nav-item">文件查看与渲染</button><button class="settings-nav-item">模型参数</button>';
 for(const control of schema.sections[0].controls){
  if(control.type==='segmented')$('settings-content').append(renderSegmentedControl(control));
  if(control.type==='workbench-background')$('settings-content').append(window.AideBackground.renderSettings({t}));
 }
});</script>'''
harness=harness.removeprefix('<script>').removesuffix('</script>')
s=s.replace('</body>','<script src="/fixture-workbench.js" defer></script></body>')
output=root/'.agent-state/workbench-polish/preview.html';output.parent.mkdir(parents=True,exist_ok=True);output.write_text(s)
print(output)
if __name__=='__main__':
 parser=argparse.ArgumentParser();parser.add_argument('--serve',type=int);args=parser.parse_args()
 if args.serve:
  class Handler(http.server.SimpleHTTPRequestHandler):
   def __init__(self,*args,**kwargs):super().__init__(*args,directory=str(web),**kwargs)
   def do_GET(self):
    if self.path.split('?')[0] == '/fixture-workbench.js':
     body=harness.encode();self.send_response(200);self.send_header('Content-Type','text/javascript');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    elif self.path.split('?')[0] in ['/','/preview.html']:
     body=output.read_bytes();self.send_response(200);self.send_header('Content-Security-Policy',"default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'");self.send_header('Content-Type','text/html; charset=utf-8');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    else:super().do_GET()
  http.server.ThreadingHTTPServer(('127.0.0.1',8786),Handler).serve_forever()
