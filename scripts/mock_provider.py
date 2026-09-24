#!/usr/bin/env python3
"""Local QA fixture only. Deterministic responses, never a real AI model.
Run inside a test container: python3 scripts/mock_provider.py
Configure the test aide instance with http://127.0.0.1:9001 and model qa-fixture.
Supports both plain JSON (stream:false) and SSE (stream:true) responses.
Do not use this endpoint as a real model provider.
"""
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def choose_content(instruction):
    if 'Generate an implementation proposal' in instruction:
        return json.dumps({'summary': 'QA 模拟提案，不是真实模型输出', 'files': [{'path': 'test-results/workflow-smoke.txt', 'content': 'aide workflow smoke passed\n'}], 'commands': ['cat test-results/workflow-smoke.txt']})
    if '制定简短的实施计划' in instruction:
        return '【QA 模拟响应】创建测试文件，等待用户应用，然后在命令面板验证。'
    if '审查上述计划' in instruction:
        return '【QA 模拟审查】提案只创建测试文件。尚未写入文件，尚未运行验证命令。'
    return '【QA 模拟对话】已验证浏览器、Go API 与模型适配器之间的请求链路；这不是真实 AI 推理。'


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        instruction = request['messages'][-1]['content']
        content = choose_content(instruction)
        if request.get('stream'):
            # SSE 流式：按 4 个字符切块逐片输出，末尾带 usage 与 [DONE]
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            time.sleep(float(os.environ.get('QA_START_DELAY', '0')))
            for i in range(0, len(content), 4):
                chunk = json.dumps({'choices': [{'delta': {'content': content[i:i + 4]}}]})
                self.wfile.write(('data: ' + chunk + '\n\n').encode())
                self.wfile.flush()
                time.sleep(float(os.environ.get('QA_DELAY', '0.02')))
            usage = json.dumps({'choices': [{'delta': {}, 'finish_reason': 'stop'}], 'usage': {'prompt_tokens': 20, 'completion_tokens': 10, 'total_tokens': 30}})
            self.wfile.write(('data: ' + usage + '\n\n').encode())
            self.wfile.write(b'data: [DONE]\n\n')
            self.wfile.flush()
            return
        body = json.dumps({'choices': [{'message': {'role': 'assistant', 'content': content}}]}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        pass  # QA fixture 保持安静


if __name__ == '__main__':
    BIND = os.environ.get('QA_BIND', '127.0.0.1')
    ThreadingHTTPServer((BIND, 9001), Handler).serve_forever()
