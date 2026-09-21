#!/usr/bin/env python3
"""Local QA fixture only. Deterministic responses, never a real AI model.
Run inside a test container: python3 scripts/mock_provider.py
Configure the test aide instance with http://127.0.0.1:9001 and model qa-fixture.
Do not use this endpoint as a real model provider.
"""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        instruction = request['messages'][-1]['content']
        if 'Generate an implementation proposal' in instruction:
            content = json.dumps({'summary': 'QA 模拟提案，不是真实模型输出', 'files': [{'path': 'test-results/workflow-smoke.txt', 'content': 'aide workflow smoke passed\n'}], 'commands': ['cat test-results/workflow-smoke.txt']})
        elif '制定简短的实施计划' in instruction:
            content = '【QA 模拟响应】创建测试文件，等待用户应用，然后在命令面板验证。'
        elif '审查上述计划' in instruction:
            content = '【QA 模拟审查】提案只创建测试文件。尚未写入文件，尚未运行验证命令。'
        else:
            content = '【QA 模拟对话】已验证浏览器、Go API 与模型适配器之间的请求链路；这不是真实 AI 推理。'
        body = json.dumps({'choices': [{'message': {'role': 'assistant', 'content': content}}]}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

if __name__ == '__main__':
    ThreadingHTTPServer(('127.0.0.1', 9001), Handler).serve_forever()
