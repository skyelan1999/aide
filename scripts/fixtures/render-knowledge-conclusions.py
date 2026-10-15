#!/usr/bin/env python3
"""Render the production timeline against exported temporary-App HTTP evidence."""
import argparse
import http.server
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WEB = ROOT / 'internal/server/web'
source = (WEB / 'starmap.html').read_text()
start = source.index('<details id="knowledge-timeline">')
end = source.index('<details class="logbook-diagnostics">', start)
timeline = source[start:end].replace('<details id="knowledge-timeline">', '<details id="knowledge-timeline" open>')
PAGE = '''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<script src="/settings-init.js"></script><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/starmap.css">
<title>结论与依据 · 生产组件验收</title><style>body{overflow:auto;min-height:100vh}#limits{position:static;max-width:840px;width:calc(100% - 32px);margin:24px auto;box-sizing:border-box}h1{font-size:20px}#timeline-results li{padding:12px;border:1px solid var(--map-line);border-radius:8px}button{min-height:36px}</style>
</head><body><details id="limits" open><summary>临时App真实HTTP结果 · 生产时间线组件</summary><p>此页面不连接生产、不调用模型。历史摘录不能代替当前来源。</p>''' + timeline + '''</details><script src="/starmap-timeline.js"></script><script src="/fixture.js"></script></body></html>'''
HARNESS = '''fetch('/comparison.json').then(r=>r.json()).then(data=>{
 const timeline=window.AideKnowledgeTimeline.create({request:async path=>path.startsWith('/timeline/compare')?data:{entries:[data.before,data.after].map(s=>({id:s.id,observed:s.observed,nodes:1,edges:0})),limit:32,bytesLimit:8388608,dropped:0}});
 timeline.sync({workspace:'fixture',code:false,revision:'1'});
});'''

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--data', type=Path, required=True)
    parser.add_argument('--port', type=int, default=8787)
    args = parser.parse_args()
    fixture = args.data.read_bytes()
    class Handler(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *a, **kw):
            super().__init__(*a, directory=str(WEB), **kw)
        def do_GET(self):
            key = self.path.split('?')[0]
            payload = {'/': ('text/html; charset=utf-8', PAGE.encode()), '/fixture.js': ('text/javascript', HARNESS.encode()), '/comparison.json': ('application/json', fixture)}.get(key)
            if payload:
                kind, body = payload
                self.send_response(200)
                self.send_header('Content-Type', kind)
                self.send_header('Content-Length', str(len(body)))
                self.send_header('Cache-Control', 'no-store')
                self.end_headers()
                self.wfile.write(body)
            else:
                super().do_GET()
    http.server.ThreadingHTTPServer(('127.0.0.1', args.port), Handler).serve_forever()
