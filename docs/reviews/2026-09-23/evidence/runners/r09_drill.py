#!/usr/bin/env python3
"""DSH-performed R09 isolated backup/restore drill for aide (not Codex acceptance).

Exercise the documented recovery runbook against the candidate binary in an
isolated temp dir with a loopback mock: produce state (session+task+pricing+
stats), back up the data dir, destroy it, restore from backup, and verify the
service recovers the exact state. No production volumes, no real model.
"""
import argparse, json, os, pathlib, shutil, subprocess, tarfile, threading, time, urllib.request, urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ALLOWED = pathlib.Path('/private/tmp/aide-independent-acceptance/frontend')


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


class Mock:
    def __init__(self):
        self.requests = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                length = int(self.headers.get('Content-Length', 0))
                req = json.loads(self.rfile.read(length))
                self.server.mock.requests.append(req)
                msgs = req.get('messages', [])
                sys_text = '\n'.join(str(m.get('content', '')) for m in msgs if m.get('role') == 'system')
                if '主题短语' in sys_text:
                    return self.complete('验收主题')
                if ('压缩' in sys_text and '摘要' in sys_text) or 'compaction' in sys_text.lower():
                    return self.complete(json.dumps({'goal': 'x', 'decisions': [], 'files': [], 'facts': [], 'pending': []}, ensure_ascii=False))
                self.complete('合成回答：备份恢复演练专用。')

            def complete(self, content):
                body = {'choices': [{'message': {'role': 'assistant', 'content': content}, 'finish_reason': 'stop'}],
                        'usage': {'prompt_tokens': 40, 'completion_tokens': 10, 'total_tokens': 50}}
                data = json.dumps(body).encode('utf-8')
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.mock = self
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class App:
    def __init__(self, binary, root, mock_url, port):
        self.binary = str(binary)
        self.root = pathlib.Path(root)
        self.data = self.root / 'data'
        self.work = self.root / 'work'
        for d in (self.data, self.work):
            d.mkdir(parents=True, exist_ok=True)
        self.mock_url = mock_url
        self.port = port
        self.proc = None
        self.captured = ''

    def start(self):
        env = dict(os.environ)
        env.update({'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'HOME': str(self.root / 'home'),
                    'AIDE_WORKSPACE': str(self.work), 'AIDE_CONTEXT': str(self.root / 'ref'),
                    'AIDE_DATA': str(self.data), 'AIDE_ADDR': '127.0.0.1:' + str(self.port),
                    'AI_BASE_URL': self.mock_url, 'AI_MODEL': 'drill-model', 'AI_API_KEY': ''})
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
        self.token = (self.data / 'access-token').read_text().strip()

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
    mock = Mock()
    app = App(args.binary, root, mock.url, 8123)
    try:
        # 1) 产生状态：会话 + 任务 + 按模型费率 + 统计
        app.start()
        st, session = app.api('POST', '/api/sessions', {'title': 'drill-session'}, status=201)
        sid = session['id']
        app.api('PUT', '/api/token-pricing', {'model': 'drill-model', 'priceIn': 2, 'priceOut': 8}, status=200)
        st, run = app.api('POST', '/api/sessions/' + sid + '/runs', {'prompt': 'drill task', 'mode': 'chat', 'attachments': [], 'strategy': 'manual'}, status=202)
        deadline = time.monotonic() + 12
        task = None
        while time.monotonic() < deadline:
            _, sess = app.api('GET', '/api/sessions/' + sid)
            task = next((r for r in sess['runs'] if r['id'] == run['id']), None)
            if task and task['status'] != 'running':
                break
            time.sleep(0.05)
        assert task and task['status'] == 'completed', task
        st, stats = app.api('GET', '/api/token-stats')
        assert stats['totals']['total'] > 0, stats
        token_before = app.token
        app.stop()
        results.append({'check': 'r09-01-produce-state', 'status': 'PASS',
                        'detail': {'session': sid, 'run': run['id'], 'tokens': stats['totals']['total'], 'token': token_before[:8] + '…'}})
        print(json.dumps(results[-1], ensure_ascii=False))

        # 2) 备份（运行手册步骤 1）
        backup = root / 'data-backup.tar.gz'
        with tarfile.open(backup, 'w:gz') as tf:
            tf.add(app.data, arcname='data')
        results.append({'check': 'r09-02-backup-created', 'status': 'PASS',
                        'detail': {'backup': str(backup), 'bytes': backup.stat().st_size}})
        print(json.dumps(results[-1], ensure_ascii=False))

        # 3) 模拟数据卷损坏（运行手册步骤 2 的对立面）
        shutil.rmtree(app.data)
        app.data.mkdir()
        results.append({'check': 'r09-03-destroyed', 'status': 'PASS', 'detail': {}})
        print(json.dumps(results[-1], ensure_ascii=False))

        # 4) 从备份恢复（运行手册步骤 3）：剥离归档内 data/ 前缀并拒绝越界路径
        with tarfile.open(backup) as tf:
            members = []
            for m in tf.getmembers():
                name = m.name[len('data/'):] if m.name.startswith('data/') else m.name
                if not name or name.startswith('/') or '..' in name.split('/'):
                    raise Exception('unsafe archive member: ' + m.name)
                m.name = name
                members.append(m)
            tf.extractall(app.data, members=members)
        assert (app.data / 'access-token').read_text().strip() == token_before, 'restored token differs'
        results.append({'check': 'r09-04-restored', 'status': 'PASS', 'detail': {'tokenMatch': True}})
        print(json.dumps(results[-1], ensure_ascii=False))

        # 5) 重启并验证状态完整恢复（运行手册步骤 4）
        app.start()
        assert (app.data / 'access-token').read_text().strip() == app.token, 'token changed across restore-restart'
        st, sess = app.api('GET', '/api/sessions/' + sid, status=200)
        assert sess['id'] == sid, sess
        assert sess['title'] == '验收主题', 'topic-updated title lost across restore: ' + str(sess.get('title'))
        runs = sess['runs']
        assert any(r['id'] == run['id'] and r['status'] == 'completed' for r in runs), runs
        st, pricing = app.api('GET', '/api/token-pricing', status=200)
        assert pricing['rates'].get('drill-model', {}) == {'priceIn': 2, 'priceOut': 8}, pricing
        st, stats2 = app.api('GET', '/api/token-stats', status=200)
        assert stats2['totals']['total'] == stats['totals']['total'], (stats['totals'], stats2['totals'])
        st, health = app.api('GET', '/healthz', status=200)
        results.append({'check': 'r09-05-recovered-service', 'status': 'PASS',
                        'detail': {'sessionIntact': True, 'runCompleted': True, 'pricingIntact': True,
                                   'statsIdentical': stats2['totals']['total'], 'health': health}})
        print(json.dumps(results[-1], ensure_ascii=False))
        app.stop()
    except AssertionError as e:
        results.append({'check': 'r09-FATAL', 'status': 'FAIL', 'detail': str(e)})
        print(json.dumps(results[-1], ensure_ascii=False))
        app.stop()
    except Exception as e:
        import traceback
        results.append({'check': 'r09-FATAL', 'status': 'FAIL', 'detail': repr(e) + ' ' + traceback.format_exc(limit=3)})
        print(json.dumps(results[-1], ensure_ascii=False))
        app.stop()
    finally:
        mock.close()
    (root / 'r09-results.json').write_text(json.dumps({'commit': args.expected_revision, 'results': results}, ensure_ascii=False, indent=2) + '\n')
    print('RESULTS_FILE ' + str(root / 'r09-results.json'))
    return 1 if any(r['status'] == 'FAIL' for r in results) else 0


if __name__ == '__main__':
    raise SystemExit(main())
