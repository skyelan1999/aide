#!/usr/bin/env python3
"""Host-side loopback mock for browser acceptance. Same behavior as the
in-container supervisor mock, but bound on the host so the browser harness can
read GET /log directly and the in-container candidate reaches it via
host.docker.internal."""
import json, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = 19093
captured = []
lock = threading.Lock()

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        if self.path == '/reset':
            with lock:
                captured.clear()
            self.send_response(200)
            self.end_headers()
            return
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
            time.sleep(3)
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

server = ThreadingHTTPServer(('127.0.0.1', PORT), Handler)
print('HOST_MOCK http://127.0.0.1:' + str(PORT), flush=True)
server.serve_forever()
