#!/usr/bin/env python3
"""DSH-performed R08 API-level verification for aide (not Codex acceptance).

Runs the candidate binary in an isolated temp dir (no production volumes, no
real keys, no real model). Assertions are drawn from r08-acceptance.md and
r08-fixtures.json. UI/browser items that need a real browser are recorded as
NOT_RUN/BLOCKED below, never as PASS. Pricing/usage math for exact fixture
numbers is covered by Go unit tests (TestR08ZeroPricingIsRealZero,
TestR08PriceSnapshotStable, TestR08LegacyMigration); this script verifies the
black-box API contract, persistence, restart immunity and migration behavior.
"""
import argparse, json, os, pathlib, shutil, subprocess, sys, time, urllib.request, urllib.error

ALLOWED = pathlib.Path('/private/tmp/aide-independent-acceptance/frontend')

def http(method, url, body=None, token=None, timeout=5):
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

class App:
    def __init__(self, binary, root):
        self.binary = str(binary)
        self.root = pathlib.Path(root)
        self.data = self.root / 'data'
        self.work = self.root / 'work'
        for d in (self.data, self.work):
            d.mkdir(parents=True, exist_ok=True)
        self.proc = None
        self.port = 8099
    def start(self):
        env = dict(os.environ)
        env.update({'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'HOME': str(self.root / 'home'),
                    'AIDE_WORKSPACE': str(self.work), 'AIDE_CONTEXT': str(self.root / 'ref'),
                    'AIDE_DATA': str(self.data), 'AIDE_ADDR': '127.0.0.1:' + str(self.port),
                    'AI_BASE_URL': 'http://127.0.0.1:1/', 'AI_MODEL': 'model-a', 'AI_API_KEY': ''})
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
                    out = self.proc.stdout.read().decode('utf-8', 'replace') if self.proc.stdout else ''
                    raise RuntimeError('binary exited early: ' + out[-800:])
                time.sleep(0.05)
        else:
            raise RuntimeError('binary did not become ready')
        # access-token 在启动时生成；config 不鉴权，必须等 token 文件就位再取
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
    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
        if self.proc and self.proc.stdout:
            self.captured = self.proc.stdout.read().decode('utf-8', 'replace')
        self.proc = None
    def logs(self):
        return getattr(self, 'captured', '')

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
            results.append({'check': label, 'status': 'FAIL', 'detail': repr(e)})
            results_line(label, 'FAIL', repr(e))

    # ── R08-01: 0 价真实 0，负值拒绝，重启后仍 0 ──
    def r08_01():
        app = App(args.binary, root / 'r08-01')
        app.start()
        try:
            st, _ = http('PUT', app.base + '/api/token-pricing', {'model': 'local-free', 'priceIn': 0, 'priceOut': 0}, token=app.token)
            assert st == 200, ('PUT 0 rate rejected', st)
            st, got = http('GET', app.base + '/api/token-pricing', token=app.token)
            assert st == 200, ('GET pricing', got)
            assert got['rates'].get('local-free', {}) == {'priceIn': 0, 'priceOut': 0}, ('0 rate did not round-trip in rates', got)
            # 活跃模型路径（UI 不带 model）：明确 0 必须被采纳，而不是回退默认 2/8
            st, _ = http('PUT', app.base + '/api/token-pricing', {'priceIn': 0, 'priceOut': 0}, token=app.token)
            assert st == 200, ('active-model PUT 0 rejected', st)
            st, got2 = http('GET', app.base + '/api/token-pricing', token=app.token)
            assert st == 200 and got2['priceIn'] == 0 and got2['priceOut'] == 0 and not got2.get('defaulted'), ('active 0 rate fell back to default', got2)
            st, _ = http('PUT', app.base + '/api/token-pricing', {'model': 'local-free', 'priceIn': -1, 'priceOut': 0}, token=app.token)
            assert st == 400, ('negative rate accepted', st)
        finally:
            app.stop()
        app.start()  # restart: 0 价必须持久化，不能被默认 2/8 覆盖
        try:
            st, got = http('GET', app.base + '/api/token-pricing', token=app.token)
            assert st == 200, ('GET after restart', got)
            assert got['rates'].get('local-free', {}) == {'priceIn': 0, 'priceOut': 0}, ('0 rate lost across restart', got)
            assert got['priceIn'] == 0 and got['priceOut'] == 0 and not got.get('defaulted'), ('active 0 rate lost across restart', got)
        finally:
            app.stop()
        return {'zeroAccepted': True, 'negativeRejected': True, 'restartStable': True}

    # ── R08-02: 按模型费率持久化；GET 契约（rates/default）；改价不动已存文件 ──
    def r08_02():
        app = App(args.binary, root / 'r08-02')
        app.start()
        try:
            st, _ = http('PUT', app.base + '/api/token-pricing', {'model': 'model-a', 'priceIn': 2, 'priceOut': 8}, token=app.token)
            assert st == 200, ('PUT model-a', st)
            st, _ = http('PUT', app.base + '/api/token-pricing', {'model': 'model-b', 'priceIn': 10, 'priceOut': 40}, token=app.token)
            assert st == 200, ('PUT model-b', st)
            st, got = http('GET', app.base + '/api/token-pricing', token=app.token)
            assert st == 200, ('GET', st)
            assert 'rates' in got and 'default' in got, ('missing rates/default contract', got)
            assert got['rates'].get('model-a', {}).get('priceIn') == 2, got
            assert got['rates'].get('model-b', {}).get('priceOut') == 40, got
            st, _ = http('PUT', app.base + '/api/token-pricing', {'model': 'model-a', 'priceIn': 20, 'priceOut': 80}, token=app.token)
            assert st == 200, ('rate change', st)
        finally:
            app.stop()
        app.start()  # 重启：费率与已存状态一致
        try:
            st, got = http('GET', app.base + '/api/token-pricing', token=app.token)
            assert st == 200 and got['rates'].get('model-a', {}).get('priceIn') == 20, ('rate change lost', got)
            st, stats = http('GET', app.base + '/api/token-stats', token=app.token)
            assert st == 200, ('stats', st)
            assert 'callRecords' in stats and 'pricing' in stats and 'estimatedCost' in stats, ('stats contract', stats)
        finally:
            app.stop()
        return {'perModelRates': True, 'changePersisted': True, 'statsContract': True}

    # ── R08-03: 旧版汇总迁移 ──
    def r08_03():
        app = App(args.binary, root / 'r08-03')
        legacy = {'version': 1, 'days': {
            '2026-09-20': {'prompt': 1200, 'completion': 300, 'total': 1500, 'calls': 2, 'estimated': True},
            '2026-09-21': {'prompt': 2400, 'completion': 600, 'total': 3000, 'calls': 3, 'estimated': False}}}
        (app.data / 'token-stats.json').write_text(json.dumps(legacy))
        def assert_stats():
            st, got = http('GET', app.base + '/api/token-stats', token=app.token)
            assert st == 200, ('stats', st)
            assert got['totals']['total'] == 4500 and got['totals']['calls'] == 5, ('totals', got['totals'])
            assert got['unpricedTotals']['total'] == 4500 and got['unpricedTotals']['calls'] == 5, ('unpriced', got['unpricedTotals'])
            assert got['cost'] == 0 and got['calls'] == 0, ('no fabricated cost/calls', got)
            days = got['days']
            assert days['2026-09-20'].get('estimated', False) is True and days['2026-09-21'].get('estimated', False) is False, ('estimated flags', days)
            assert days['2026-09-20'].get('priced', False) is False, ('legacy day priced', days)
        app.start()
        try:
            assert_stats()
        finally:
            app.stop()
        app.start()  # 重启不重复累加、不清零
        try:
            assert_stats()
        finally:
            app.stop()
        return {'usagePreserved': True, 'estimatedFlags': True, 'unpricedMarked': True, 'restartIdempotent': True}

    # ── R08-03b: 损坏统计文件：启动不失败、文件保留、不静默清零到磁盘 ──
    def r08_03b():
        app = App(args.binary, root / 'r08-03b')
        damaged = b'{"days": {"2026-09-19": {"prompt": 1, TRUNCATED'
        (app.data / 'token-stats.json').write_bytes(damaged)
        app.start()
        try:
            st, got = http('GET', app.base + '/api/token-stats', token=app.token)
            assert st == 200, ('stats endpoint down', st)
            assert (app.data / 'token-stats.json').read_bytes() == damaged, 'corrupt evidence not preserved'
        finally:
            app.stop()
        log_text = app.logs()
        (root / 'r08-03b' / 'app-logs.txt').write_text(log_text)
        assert 'token-stats.json' in log_text, 'no diagnostic log for corrupt stats file (see app-logs.txt, len=%d)' % len(log_text)
        return {'serviceAvailable': True, 'evidencePreserved': True, 'diagnosable': True}

    check('R08-01-zero-price', r08_01)
    check('R08-02-per-model-rates', r08_02)
    check('R08-03-legacy-migration', r08_03)
    check('R08-03b-corrupt-preserved', r08_03b)

    # R08-04 上下文预览已实现并另行验收（r08_04_api_checks.py 4/4 + 真实浏览器 12/12），
    # 此处不再重复登记 BLOCKED；浏览器级证据见 browser-harness 证据目录。

    out = {'commit': args.expected_revision, 'results': results}
    (root / 'r08-results.json').write_text(json.dumps(out, ensure_ascii=False, indent=2) + '\n')
    print('RESULTS_FILE ' + str(root / 'r08-results.json'))
    return 1 if any(r['status'] == 'FAIL' for r in results) else 0

if __name__ == '__main__':
    raise SystemExit(main())
