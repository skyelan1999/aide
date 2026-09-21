#!/usr/bin/env python3
"""Run inside aide: verifies API and persistence without contacting any model.
docker compose exec -T aide python3 /workspace/scripts/verify_runtime.py
docker compose restart aide
docker compose exec -T aide python3 /workspace/scripts/verify_runtime.py --check
"""
import json
import pathlib
import sys
import urllib.request
import urllib.error

TOKEN = pathlib.Path('/data/access-token').read_text().strip()
RECORD = pathlib.Path('/workspace/test-results/runtime-session.json')

def api(path, method='GET', body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request('http://127.0.0.1:8080' + path, data=data, method=method,
        headers={'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)

assert api('/healthz')['status'] == 'ok'
assert api('/api/config')['name'] == 'aide'
if '--check' in sys.argv:
    saved = json.loads(RECORD.read_text())
    current = api('/api/sessions/' + saved['id'])
    assert current['title'] == saved['title']
    assert api('/api/file?path=test-results/runtime-file.txt')['content'] == 'runtime persistence verified\n'
    print('PASS: token, session and workspace file survived container restart')
else:
    if RECORD.exists():
        session = api('/api/sessions/' + json.loads(RECORD.read_text())['id'])
    else:
        session = api('/api/sessions', 'POST', {'title': '部署验证（无模型调用）'})
    version = ''
    if pathlib.Path('/workspace/test-results/runtime-file.txt').exists():
        version = api('/api/file?path=test-results/runtime-file.txt')['hash']
    api('/api/file', 'PUT', {'path': 'test-results/runtime-file.txt', 'content': 'runtime persistence verified\n', 'hash': version})
    RECORD.parent.mkdir(exist_ok=True)
    RECORD.write_text(json.dumps({'id': session['id'], 'title': session['title']}))
    assert pathlib.Path('/workspace/test-results/runtime-file.txt').read_text() == 'runtime persistence verified\n'
    print('PASS: authenticated API, persistent session and bind-mounted file write')
