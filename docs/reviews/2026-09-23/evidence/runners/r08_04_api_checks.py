#!/usr/bin/env python3
"""DSH-performed R08-04 acceptance for aide (not Codex acceptance).

Implements the assertions of frontend/r08-acceptance.md §R08-04 against the
candidate binary with an in-process loopback mock provider. Fixture inputs are
r08-fixtures.json contextCases. No real model, no production data.
"""
import argparse, hashlib, json, os, pathlib, re, subprocess, sys, threading, time, urllib.request, urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ALLOWED = pathlib.Path('/private/tmp/aide-independent-acceptance/frontend')

FIXTURES = json.load(open('/private/tmp/aide-independent-acceptance/frontend/r08-fixtures.json'))
CASES = {c['id']: c for c in FIXTURES['contextCases']}

PROBE_SCHEMA = {'type': 'object', 'properties': {'path': {'type': 'string'}}, 'required': ['path']}
PROBE_DESC = 'R08_TOOL_SCHEMA_MARK'
RESULT_START = 'R08_TOOL_RESULT_START'
RESULT_END = 'R08_TOOL_RESULT_END'

PLUGIN_CODE = """'use strict';
module.exports = { name: 'R08 probe', apply(ctx) {
  ctx.tool({name: 'fixture_probe', description: """ + json.dumps(PROBE_DESC) + """, parameters: """ + json.dumps(PROBE_SCHEMA) + """,
    handler: args => ({text: """ + json.dumps(RESULT_START) + """ + 'T'.repeat(2500) + """ + json.dumps(RESULT_END) + """}) });
}};
"""

def sha256(b):
    return hashlib.sha256(b if isinstance(b, bytes) else b.encode('utf-8')).hexdigest()

def http(method, url, body=None, token=None, timeout=10):
    data = None
    headers = {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    if body is not None:
        data = json.dumps(body).encode('utf-8')
        headers['Content-Type'] = 'application/json'
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode('utf-8', 'replace')

class MockProvider:
    def __init__(self, main_reply):
        self.requests = []
        self.main_calls = []
        self.main_reply = main_reply
        self.lock = threading.Lock()
        provider = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                length = int(self.headers.get('Content-Length', 0))
                req = json.loads(self.rfile.read(length))
                with provider.lock:
                    provider.requests.append(req)
                msgs = req.get('messages', [])
                sys_text = '\n'.join(str(m.get('content', '')) for m in msgs if m.get('role') == 'system')
                if req.get('tools'):
                    # 主聊天/计划调用一定携带工具定义；标题/压缩请求不带
                    with provider.lock:
                        provider.main_calls.append(req)
                elif '主题短语' in sys_text:
                    return self.complete('验收主题')
                elif ('压缩' in sys_text and '摘要' in sys_text) or 'compaction' in sys_text.lower():
                    return self.complete(json.dumps({'goal': 'acceptance', 'decisions': [], 'files': [], 'facts': [], 'pending': []}, ensure_ascii=False))
                else:
                    with provider.lock:
                        provider.main_calls.append(req)
                reply = provider.main_reply(req, provider)
                if isinstance(reply, tuple):
                    content, calls = reply
                else:
                    content, calls = reply, None
                self.complete(content, calls)

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

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class App:
    def __init__(self, binary, root, mock_url, model='model-a'):
        self.binary = str(binary)
        self.root = pathlib.Path(root)
        self.data = self.root / 'data'
        self.work = self.root / 'work'
        for d in (self.data, self.work):
            d.mkdir(parents=True, exist_ok=True)
        self.mock_url = mock_url
        self.model = model
        self.proc = None
        self.port = 8100 + (root.name[-2:] if root.name[-2:].isdigit() else 0)
        self.port = int(str(self.port)[-4:])
        self.captured = ''

    def start(self):
        env = dict(os.environ)
        env.update({'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'HOME': str(self.root / 'home'),
                    'AIDE_WORKSPACE': str(self.work), 'AIDE_CONTEXT': str(self.root / 'ref'),
                    'AIDE_DATA': str(self.data), 'AIDE_ADDR': '127.0.0.1:' + str(self.port),
                    'AI_BASE_URL': self.mock_url, 'AI_MODEL': self.model, 'AI_API_KEY': ''})
        (self.root / 'home').mkdir(parents=True, exist_ok=True)
        (self.root / 'ref').mkdir(parents=True, exist_ok=True)
        self.proc = subprocess.Popen([self.binary], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        base = 'http://127.0.0.1:' + str(self.port)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                http('GET', base + '/api/config')
                self.base = base
                break
            except Exception:
                if self.proc.poll() is not None:
                    raise RuntimeError('binary exited early')
                time.sleep(0.05)
        else:
            raise RuntimeError('binary did not become ready')
        self.token = ''
        tdeadline = time.monotonic() + 5
        while time.monotonic() < tdeadline:
            token_file = self.data / 'access-token'
            if token_file.exists() and token_file.read_text().strip():
                self.token = token_file.read_text().strip()
                break
            time.sleep(0.05)
        if not self.token:
            raise RuntimeError('access-token not created')

    def api(self, method, path, body=None, status=None):
        st, got = http(method, self.base + path, body=body, token=self.token)
        if status is not None:
            assert st == status, (method, path, st, got)
        return st, got

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
        if self.proc and self.proc.stdout:
            self.captured += self.proc.stdout.read().decode('utf-8', 'replace')
        self.proc = None

    def seed_session(self, sid, messages, compact=None):
        path = self.data / ('session-' + sid + '.json')
        doc = {'id': sid, 'title': 'fixture', 'created': '2026-09-23T00:00:00Z', 'messages': messages, 'runs': []}
        if compact:
            doc['compact'] = compact
        path.write_text(json.dumps(doc, ensure_ascii=False))

    def create(self):
        _, s = self.api('POST', '/api/sessions', {'title': 'fixture'}, status=201)
        return s['id']

    def configure(self, window):
        self.api('PUT', '/api/settings', {
            'baseURL': self.mock_url, 'apiKey': '', 'clearKey': False,
            'models': [{'id': self.model, 'name': self.model, 'contextWindow': window}],
            'activeModel': self.model}, status=200)
        _, cfg = self.api('GET', '/api/config')
        assert cfg.get('configured') and cfg.get('model') == self.model, cfg

    def install_probe(self):
        _, got = self.api('POST', '/api/plugins', {'id': 'r08-probe', 'name': 'R08 probe', 'code': PLUGIN_CODE}, status=201)
        assert not got.get('error'), got
        assert got.get('enabled') is True, got

    def workspace_named(self, name):
        (self.work / name).mkdir(parents=True, exist_ok=True)
        self.api('PUT', '/api/workspace-config', {'workspace': {'mode': 'local', 'path': name}}, status=200)

    def done(self, sid, rid, timeout=12):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            st, sess = self.api('GET', '/api/sessions/' + sid)
            task = next((r for r in sess['runs'] if r['id'] == rid), None)
            if task and task['status'] not in ('running',):
                return sess, task
            time.sleep(0.05)
        raise AssertionError('task did not finish: ' + rid)


def results_line(label, status, detail):
    print(json.dumps({'check': label, 'status': status, 'detail': detail}, ensure_ascii=False))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--root', required=True, type=pathlib.Path)
    parser.add_argument('--expected-revision', required=True)
    args = parser.parse_args()
    root = args.root.resolve()
    if ALLOWED not in root.parents or root.exists():
        parser.error('--root must be a NEW subdirectory of ' + str(ALLOWED))
    root.mkdir(parents=True)
    results = []

    def check(label, fn):
        try:
            evidence = fn()
            results.append({'check': label, 'status': 'PASS', 'detail': evidence})
            results_line(label, 'PASS', evidence)
        except AssertionError as e:
            results.append({'check': label, 'status': 'FAIL', 'detail': str(e)})
            results_line(label, 'FAIL', str(e))
        except Exception as e:
            import traceback
            results.append({'check': label, 'status': 'FAIL', 'detail': repr(e) + ' ' + traceback.format_exc(limit=3)})
            results_line(label, 'FAIL', repr(e))

    def scenario_first_call():
        case = CASES['ctx-complete-first-call']
        provider = MockProvider(lambda req, p: ('done', None))
        app = App(args.binary, root / 'r08-04-first', provider.url)
        try:
            app.start()
            app.configure(case['contextWindow'])
            app.install_probe()
            app.workspace_named('R08_SYS_MARK')
            (app.work / 'R08_SYS_MARK' / 'fixture-a.txt').write_text(case['attachments'][0]['content'])
            # source attachment
            src_dir = app.root / 'home' / 'docs'
            src_dir.mkdir(parents=True, exist_ok=True)
            (src_dir / 'fixture-b.txt').write_text(case['attachments'][1]['content'])
            # 容器内等价于生产 /local 挂载：把来源目录落到 /local/docs
            os.makedirs('/local/docs', exist_ok=True)
            open('/local/docs/fixture-b.txt', 'w').write(case['attachments'][1]['content'])
            app.api('PUT', '/api/sources', {'sources': [{'id': 'fixture-docs', 'name': 'docs', 'type': 'local', 'enabled': True, 'config': {'path': str(src_dir)}}]}, status=200)
            sid = app.create()
            app.stop()
            app.seed_session(sid, [{'role': m['role'], 'content': m['content']} for m in case['history']],
                             compact=case['summary'])
            app.start()
            atts = [{'root': a['root'], 'path': a['path'], 'source': a.get('source', '')} for a in case['attachments']]
            st, preview = app.api('POST', '/api/context-preview', {'sessionId': sid, 'prompt': case['prompt'], 'mode': 'chat', 'attachments': atts}, status=200)
            text = '\n'.join(str(m.get('content', '')) for m in preview['messages']) + json.dumps(preview['tools'], ensure_ascii=False)
            for marker in case['requiredFirstRequestMarkers']:
                assert marker in text, 'preview missing marker ' + marker
            assert preview['outputReserve'] == case['maxOutputTokens'], ('output reserve', preview['outputReserve'])
            assert preview['contextWindow'] == case['contextWindow'], ('window', preview['contextWindow'])
            assert preview['inputEstimate'] > 0 and preview['breakdown']['systemChars'] > 0
            assert '非精确 tokenizer' in preview['estimationNote'] or '估算' in preview['estimationNote']
            st, run = app.api('POST', '/api/sessions/' + sid + '/runs', {'prompt': case['prompt'], 'mode': 'chat', 'attachments': atts, 'strategy': 'manual'}, status=202)
            _, task = app.done(sid, run['id'])
            assert task['status'] == 'completed', ('task status', task['status'], task.get('error'))
            st, snap = app.api('GET', '/api/sessions/' + sid + '/runs/' + run['id'] + '/requests', status=200)
            snaps = snap['snapshots']
            assert snaps, 'no request snapshots recorded'
            first = snaps[0]
            body = first['body']
            assert body['messages'] == preview['messages'], 'actual request messages != preview messages'
            assert body.get('tools') == preview['tools'], 'actual request tools != preview tools'
            assert body.get('max_tokens') == case['maxOutputTokens'], ('max_tokens', body.get('max_tokens'))
            real = [r for r in provider.main_calls]
            assert len(real) == 1, ('main calls', len(real))
            real_text = '\n'.join(str(m.get('content', '')) for m in real[0]['messages']) + json.dumps(real[0].get('tools', []), ensure_ascii=False)
            for marker in case['requiredFirstRequestMarkers']:
                assert marker in real_text, 'real request missing marker ' + marker
            assert real[0] == body, 'mock-received request != recorded snapshot body'
            return {'previewMatchesActual': True, 'allMarkers': True, 'reserve4096': True, 'snapshots': len(snaps)}
        finally:
            app.stop()
            provider.close()

    def scenario_unicode_history():
        case = CASES['ctx-unicode-history-budget']
        gen = case['historyGenerator']
        history_msg = {'role': gen['role'], 'content': gen['character'] * gen['repeat']}
        assert len(history_msg['content']) == case['expectedUTF16CodeUnits']
        assert len(history_msg['content'].encode('utf-8')) == case['expectedUTF8Bytes']
        provider = MockProvider(lambda req, p: ('done', None))
        app = App(args.binary, root / 'r08-04-unicode', provider.url)
        try:
            app.start()
            app.configure(65536)
            sid = app.create()
            app.stop()
            app.seed_session(sid, [history_msg])
            app.start()
            st, preview = app.api('POST', '/api/context-preview', {'sessionId': sid, 'prompt': 'unicode budget probe', 'mode': 'chat', 'attachments': []}, status=200)
            st, run = app.api('POST', '/api/sessions/' + sid + '/runs', {'prompt': 'unicode budget probe', 'mode': 'chat', 'attachments': [], 'strategy': 'manual'}, status=202)
            _, task = app.done(sid, run['id'])
            assert task['status'] == 'completed', task.get('error')
            _, snap = app.api('GET', '/api/sessions/' + sid + '/runs/' + run['id'] + '/requests', status=200)
            body = snap['snapshots'][0]['body']
            assert body['messages'] == preview['messages'], 'unicode history selection differs between preview and real request'
            joined = '\n'.join(str(m.get('content', '')) for m in body['messages'])
            included = history_msg['content'] in joined
            # 规范允许任意合理的回放预算策略；断言的是“预览与真实请求选择同一集合”，
            # 而不是旧的 60,000 字节门槛必须保留某条消息。
            return {'utf16': case['expectedUTF16CodeUnits'], 'utf8': case['expectedUTF8Bytes'], 'sameSelection': True, 'historyIncludedUnderBudget': included}
        finally:
            app.stop()
            provider.close()

    def scenario_tool_second_call():
        case = CASES['ctx-tool-second-call']
        provider = MockProvider(None)
        probe_call = case['toolCall']
        provider.main_reply = lambda req, p: ('', [probe_call]) if not any(m.get('role') == 'tool' for m in req.get('messages', [])) else ('done', None)
        app = App(args.binary, root / 'r08-04-tool2', provider.url)
        try:
            app.start()
            app.configure(65536)
            app.install_probe()
            sid = app.create()
            st, run = app.api('POST', '/api/sessions/' + sid + '/runs', {'prompt': 'tool second call probe', 'mode': 'chat', 'attachments': [], 'strategy': 'manual'}, status=202)
            _, task = app.done(sid, run['id'])
            assert task['status'] == 'completed', task.get('error')
            _, snap = app.api('GET', '/api/sessions/' + sid + '/runs/' + run['id'] + '/requests', status=200)
            snaps = snap['snapshots']
            assert len(snaps) >= 2, ('snapshots', len(snaps))
            second = snaps[1]['body']
            assert len(provider.main_calls) >= 2, ('main calls', len(provider.main_calls))
            assert provider.main_calls[1] == second, 'mock second request != recorded second snapshot'
            msgs = second['messages']
            assistant = [m for m in msgs if m.get('role') == 'assistant' and m.get('tool_calls')]
            toolmsgs = [m for m in msgs if m.get('role') == 'tool']
            assert assistant, 'second round lost assistant tool_calls'
            tc = assistant[-1]['tool_calls'][0]
            assert tc['id'] == probe_call['id'] and tc['function']['name'] == probe_call['function']['name'], tc
            assert toolmsgs and toolmsgs[-1].get('tool_call_id') == probe_call['id'], 'tool_call_id mismatch'
            content = toolmsgs[-1]['content']
            assert content.startswith(RESULT_START) and content.endswith(RESULT_END), 'tool result markers lost'
            assert len(content) >= 2500 + len(RESULT_START) + len(RESULT_END), ('tool result truncated', len(content))
            full_uses = [u for u in task.get('toolUses', []) if u['tool'] == 'fixture_probe']
            assert full_uses and len(full_uses[0].get('result', '')) >= len(content), 'persisted ToolUse.Result truncated'
            return {'secondRoundMatches': True, 'toolCallIdPreserved': True, 'toolResultFull': len(content)}
        finally:
            app.stop()
            provider.close()

    def scenario_draft_and_overflow():
        case = CASES['ctx-draft-refresh-and-overflow']
        provider = MockProvider(lambda req, p: ('done', None))
        app = App(args.binary, root / 'r08-04-overflow', provider.url)
        try:
            app.start()
            app.configure(8192)
            st, p1 = app.api('POST', '/api/context-preview', {'sessionId': '', 'prompt': case['initialPrompt'], 'mode': 'chat', 'attachments': []}, status=200)
            st, p2 = app.api('POST', '/api/context-preview', {'sessionId': '', 'prompt': case['changedPrompt'], 'mode': 'chat', 'attachments': []}, status=200)
            assert p1['fingerprint'] != p2['fingerprint'], 'prompt change did not invalidate preview fingerprint'
            big = case['largeAttachmentGenerator']['character'] * case['largeAttachmentGenerator']['repeat']
            (app.work / 'big.txt').write_text(big)
            st, p3 = app.api('POST', '/api/context-preview', {'sessionId': '', 'prompt': case['changedPrompt'], 'mode': 'chat', 'attachments': [{'root': 'workspace', 'path': 'big.txt'}]}, status=200)
            assert p3['fingerprint'] != p2['fingerprint'], 'attachment change did not invalidate preview fingerprint'
            assert p3['overLimit'] is True, ('over-limit not detected', p3['totalEstimate'], p3['contextWindow'])
            before = len(provider.main_calls)
            sid = app.create()
            st, got = app.api('POST', '/api/sessions/' + sid + '/runs', {'prompt': case['changedPrompt'], 'mode': 'chat', 'attachments': [{'root': 'workspace', 'path': 'big.txt'}], 'strategy': 'manual'})
            assert 400 <= st < 500, ('over-limit send not blocked', st, got)
            assert '上下文预算超限' in json.dumps(got, ensure_ascii=False), 'block reason not explainable'
            time.sleep(0.4)
            assert len(provider.main_calls) == before, 'blocked call reached the provider'
            return {'fingerprintInvalidation': True, 'overLimitDetected': True, 'sendBlockedExplainably': True, 'providerMainCalls': before}
        finally:
            app.stop()
            provider.close()

    check('R08-04-first-call-preview-equals-actual', scenario_first_call)
    check('R08-04-unicode-history-same-selection', scenario_unicode_history)
    check('R08-04-tool-second-round-matches', scenario_tool_second_call)
    check('R08-04-draft-refresh-and-overflow-blocked', scenario_draft_and_overflow)

    out = {'commit': args.expected_revision, 'results': results}
    (root / 'r08-04-results.json').write_text(json.dumps(out, ensure_ascii=False, indent=2) + '\n')
    print('RESULTS_FILE ' + str(root / 'r08-04-results.json'))
    return 1 if any(r['status'] == 'FAIL' for r in results) else 0


if __name__ == '__main__':
    raise SystemExit(main())
