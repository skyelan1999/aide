#!/usr/bin/env python3
"""Supervisor for the browser acceptance: starts the loopback mock provider and
the candidate binary in the same container, then idles. The mock records every
provider request body and serves them at GET /log for the browser harness to
compare with the context preview.
"""
import json, os, subprocess, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MOCK_PORT = 19090
AIDE_PORT = 8097
BIN = sys.argv[1] if len(sys.argv) > 1 else '/usr/local/bin/aide'
ROOT = os.environ.get('BROWSER_RUN', '/browser-run')

os.makedirs(ROOT, exist_ok=True)
for sub in ('data', 'work', 'ref', 'home', 'evidence'):
    os.makedirs(os.path.join(ROOT, sub), exist_ok=True)

captured = []
lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        if self.path == '/log':
            with lock:
                body = json.dumps(captured).encode('utf-8')
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        req = json.loads(self.rfile.read(length))
        with lock:
            captured.append(req)
        msgs = req.get('messages', [])
        sys_text = '\n'.join(str(m.get('content', '')) for m in msgs if m.get('role') == 'system')
        text = '\n'.join(str(m.get('content', '')) for m in msgs)
        if '主题短语' in sys_text:
            return self.complete('验收主题')
        if ('压缩' in sys_text and '摘要' in sys_text) or 'compaction' in sys_text.lower():
            return self.complete(json.dumps({'goal': 'acceptance', 'decisions': [], 'files': [], 'facts': [], 'pending': []}, ensure_ascii=False))
        if 'SLOW_MARKER' in text:
            time.sleep(3)  # 拖住任务 A，让浏览器先切到会话 B
        if 'TOOL_PROBE' in text and not any(m.get('role') == 'tool' for m in msgs):
            call = {'id': 'browser-probe-1', 'type': 'function',
                    'function': {'name': 'list_files', 'arguments': json.dumps({'path': '.'})}}
            return self.complete('', [call])
        self.complete('验收响应：这是隔离 Mock 的合成回答，不调用任何真实模型。')

    def complete(self, content, calls=None):
        msg = {'role': 'assistant', 'content': content}
        if calls is not None:
            msg['tool_calls'] = calls
        body = {'choices': [{'message': msg, 'finish_reason': 'tool_calls' if calls else 'stop'}],
                'usage': {'prompt_tokens': 40, 'completion_tokens': 10, 'total_tokens': 50}}
        data = json.dumps(body).encode('utf-8')
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)


server = ThreadingHTTPServer(('127.0.0.1', MOCK_PORT), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()

env = dict(os.environ)
env.update({
    'HOME': os.path.join(ROOT, 'home'),
    'AIDE_WORKSPACE': os.path.join(ROOT, 'work'),
    'AIDE_CONTEXT': os.path.join(ROOT, 'ref'),
    'AIDE_DATA': os.path.join(ROOT, 'data'),
    'AIDE_ADDR': '0.0.0.0:' + str(AIDE_PORT),
    'AI_BASE_URL': 'http://127.0.0.1:' + str(MOCK_PORT),
    'AI_MODEL': 'browser-model',
    'AI_API_KEY': '',
})
proc = subprocess.Popen([BIN], env=env)
print('MOCK_URL http://127.0.0.1:' + str(MOCK_PORT))
print('AIDE_URL http://127.0.0.1:' + str(AIDE_PORT))
sys.stdout.flush()
try:
    while proc.poll() is None:
        time.sleep(0.5)
finally:
    server.shutdown()
