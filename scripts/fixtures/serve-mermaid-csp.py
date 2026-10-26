#!/usr/bin/env python3
"""Serve a strict-CSP Mermaid fixture; proof writes need this run's token.

This protects against cross-origin browser forgery, not a hostile local process
that can read the fixture/token or modify the operator's proof file directly.
"""
import argparse
import json
import os
import secrets
import tempfile
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
HTML = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Mermaid CSP 验收</title><link rel="stylesheet" href="/style.css"><link rel="stylesheet" href="/fixture.css"><script defer src="/vendor/mermaid.min.js"></script><script defer src="/markdown-diagrams.js"></script><script defer src="/fixture.js"></script><body><h1>Mermaid · 严格 CSP 配色验收</h1><button id="theme">切换明暗</button><pre id="result">运行中</pre><main class="md-body" id="diagrams"></main></body></html>'''
CSS = '''body{display:block;overflow:auto;height:auto;padding:24px;background:var(--bg);color:var(--text);font-family:system-ui}main{max-width:1000px;margin:auto}h1{font-size:22px}pre{white-space:pre-wrap}button{padding:8px 16px}.mermaid{min-height:100px}'''
JS = r'''(async()=>{
 const proofToken = "AIDE_PROOF_TOKEN";
 const saveProof=proof=>fetch('/proof',{method:'POST',headers:{'Content-Type':'application/json','X-Aide-Proof-Token':proofToken},body:JSON.stringify(proof)}).then(response=>{if(!response.ok)throw Error('Proof write rejected: '+response.status);});
 const examples=[
 'flowchart LR\n A([开始])-->B[读取文档]\n B-->C{检查通过?}\n C--是-->D[输出结果]\n C--否-->B\n classDef custom fill:#a4e6c3,stroke:#18764a,color:#123d28\n class D custom',
 'sequenceDiagram\n participant A as 用户\n participant B as 工作台\n A->>B: 打开文档\n B-->>A: 显示预览',
 'classDiagram\n class Document {\n +String name\n +render()\n }\n Document --> Preview',
 'stateDiagram-v2\n [*] --> 加载\n 加载 --> 就绪\n 就绪 --> [*]'];
 const host=document.querySelector('main'),out=document.querySelector('#result');
 for(const text of examples){const div=document.createElement('div');div.className='mermaid';div.textContent=text;host.append(div);}
 const initial= document.adoptedStyleSheets.length;
 const frames=()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
 const checks=[];
 for(const dark of [false,true,false]){
  document.documentElement.dataset.theme=dark?'dark':'light';
  await AideMarkdownDiagrams.render(host,{load:async()=>{},dark,strict:true});await frames();
  const node=host.querySelector('.node rect.basic'),custom=host.querySelector('.node.custom rect'),line=host.querySelector('.flowchart-link');
  const fill=getComputedStyle(node).fill,author=getComputedStyle(custom).fill,stroke=getComputedStyle(line).stroke;
  if(fill!==(dark?'rgb(36, 60, 85)':'rgb(234, 242, 255)'))throw Error('Node colour: '+fill);
  if(author!=='rgb(164, 230, 195)')throw Error('Author class: '+author);
  if(stroke!==(dark?'rgb(161, 183, 206)':'rgb(82, 111, 139)'))throw Error('Edge colour: '+stroke);
  if(host.querySelectorAll('svg').length!==4)throw Error('Missing diagrams');
  if(document.adoptedStyleSheets.length!==initial+4)throw Error('Sheet leak on theme change');
  if(!host.querySelector('svg style').textContent.includes('primary')&&!host.querySelector('svg style').textContent.includes('fill:'))throw Error('Export CSS missing');
  checks.push({dark,fill,author,stroke,diagrams:4});
 }
 const saved=[...host.children];host.replaceChildren();await frames();
 if(document.adoptedStyleSheets.length!==initial)throw Error('Detached sheet leak');
 host.append(...saved);await AideMarkdownDiagrams.render(host,{load:async()=>{},dark:false,strict:true});await frames();
 if(document.adoptedStyleSheets.length!==initial+4)throw Error('Reattach CSS missing');
 // Reattached diagrams must reinstall their stylesheet even when theme is unchanged.
 await AideMarkdownDiagrams.render(host,{load:async()=>{},dark:false,strict:true});await frames();
 const proof={status:'pass',strictCSP:true,checks,detachedCleanup:true,svgTextLabels:host.querySelectorAll('foreignObject').length===0};
 out.textContent='PASS · 明暗配色／作者颜色／四种图表／重复渲染与资源释放';
 await saveProof(proof);
 document.querySelector('button').onclick=async()=>{const dark=document.documentElement.dataset.theme!=='dark';document.documentElement.dataset.theme=dark?'dark':'light';await AideMarkdownDiagrams.render(host,{load:async()=>{},dark});};
})().catch(async e=>{document.querySelector('#result').textContent='FAIL · '+e.message;await fetch('/proof',{method:'POST',headers:{'Content-Type':'application/json','X-Aide-Proof-Token':'AIDE_PROOF_TOKEN'},body:JSON.stringify({status:'fail',error:e.message})});});'''


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--port', type=int, default=8767)
    p.add_argument('--proof', type=Path, default=ROOT / '.agent-state/mermaid-black/browser.json')
    p.add_argument('--renderer', type=Path, default=ROOT / 'internal/server/web/markdown-diagrams.js')
    args = p.parse_args()
    proof_token = secrets.token_urlsafe(32)
    fixture_js = JS.replace('AIDE_PROOF_TOKEN', proof_token)
    expected_origin = None
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            files = {'/': ('text/html', HTML.encode()), '/fixture.css': ('text/css', CSS.encode()), '/fixture.js': ('text/javascript', fixture_js.encode())}
            path = self.path.split('?')[0]
            if path == '/markdown-diagrams.js':
                result = ('text/javascript', args.renderer.read_bytes())
            elif path in ('/style.css', '/vendor/mermaid.min.js'):
                result = ('text/css' if path.endswith('.css') else 'text/javascript', (ROOT / 'internal/server/web' / path.lstrip('/')).read_bytes())
            else:
                result = files.get(path)
            if result is None:
                self.send_error(404); return
            mime, data = result
            self.send_response(200)
            self.send_header('Content-Type', mime + '; charset=utf-8')
            self.send_header('Content-Security-Policy', "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'")
            self.send_header('Cache-Control', 'no-store')
            self.send_header('Cross-Origin-Resource-Policy', 'same-origin')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.end_headers(); self.wfile.write(data)
        def do_POST(self):
            if self.path != '/proof':
                self.send_error(404); return
            if (self.headers.get('Host') != expected_origin.removeprefix('http://') or
                    self.headers.get('Origin') not in (None, expected_origin) or
                    not secrets.compare_digest(self.headers.get('X-Aide-Proof-Token', '').encode('utf-8'), proof_token.encode('ascii'))):
                self.send_error(403); return
            if self.headers.get_content_type() != 'application/json':
                self.send_error(415); return
            try:
                size = int(self.headers.get('Content-Length', ''))
                if size < 0:
                    raise ValueError('negative body size')
            except ValueError:
                self.send_error(400); return
            if size > 16384:
                self.send_error(413); return
            try:
                proof = json.loads(self.rfile.read(size))
                if not isinstance(proof, dict):
                    raise ValueError('proof must be an object')
            except (ValueError, UnicodeError):
                self.send_error(400); return
            args.proof.parent.mkdir(parents=True, exist_ok=True)
            temporary = None
            try:
                with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=args.proof.parent,
                                                 prefix='.mermaid-proof-', delete=False) as output:
                    temporary = Path(output.name)
                    output.write(json.dumps(proof, ensure_ascii=False, indent=2) + '\n')
                os.replace(temporary, args.proof)
            finally:
                if temporary is not None:
                    temporary.unlink(missing_ok=True)
            self.send_response(204); self.end_headers()
    server = HTTPServer(('127.0.0.1', args.port), Handler)
    expected_origin = f'http://127.0.0.1:{server.server_port}'
    print(expected_origin + '/', flush=True)
    server.serve_forever()

if __name__ == '__main__':
    main()
