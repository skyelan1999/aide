#!/usr/bin/env python3
"""QA fixture：模拟带工具调用的流式对话（chat 模式验证 live 工具行与轮次重置）。
stream 请求第 1 轮：文本叙述 + list_files 工具调用；第 2 轮：最终回答。
"""
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        if not request.get('stream'):
            body = json.dumps({'choices': [{'message': {'role': 'assistant', 'content': '主题'}}]}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        has_tool = any(m.get('role') == 'tool' for m in request.get('messages', []))
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.end_headers()
        time.sleep(float(os.environ.get('QA_START_DELAY', '1')))
        delay = float(os.environ.get('QA_DELAY', '0.15'))
        if not has_tool:
            # 第 1 轮：叙述 + 工具调用
            for piece in ['让我', '先查看', '一下工作', '目录。']:
                self.wfile.write(('data: ' + json.dumps({'choices': [{'delta': {'content': piece}}]}) + '\n\n').encode())
                self.wfile.flush()
                time.sleep(delay)
            calls = [
                {'index': 0, 'id': 'call_qa1', 'type': 'function', 'function': {'name': 'list_files', 'arguments': ''}},
                {'index': 0, 'function': {'arguments': '{"source":"","path":"."}'}},
            ]
            for c in calls:
                self.wfile.write(('data: ' + json.dumps({'choices': [{'delta': {'tool_calls': [c]}}]}) + '\n\n').encode())
                self.wfile.flush()
                time.sleep(delay)
            self.wfile.write(b'data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}\n\n')
        else:
            # 第 2 轮：最终回答（验证 live 按轮次重置，不混入第 1 轮叙述）
            for piece in ['目录里', '有 ', 'README。', '文件已读', '取完成。']:
                self.wfile.write(('data: ' + json.dumps({'choices': [{'delta': {'content': piece}}]}) + '\n\n').encode())
                self.wfile.flush()
                time.sleep(delay)
            self.wfile.write(b'data: {"choices":[{"delta":{},"finish_reason":"stop"}]}\n\n')
        self.wfile.write(b'data: [DONE]\n\n')
        self.wfile.flush()

    def log_message(self, format, *args):
        pass


if __name__ == '__main__':
    ThreadingHTTPServer((os.environ.get('QA_BIND', '127.0.0.1'), 9002), Handler).serve_forever()
