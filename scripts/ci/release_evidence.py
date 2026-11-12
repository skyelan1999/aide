#!/usr/bin/env python3
"""Validate actual execution receipts, independently of successful job labels."""
import itertools
import json
from pathlib import Path


def require(ok, message):
    if not ok:
        raise ValueError(message)


def go_suite(report):
    require(report.get('status') == 'pass', 'Missing complete race/packages/platform/vet result')
    server = report.get('server', {})
    names = server.get('inventory', [])
    require(server.get('status') == 'pass' and names and len(names) == len(set(names)) == server.get('inventoryCount'), 'Invalid runtime test inventory')
    seen = []
    for shard in server.get('shards', []):
        require(shard.get('exit') == 0 and not shard.get('missing'), 'Incomplete Go shard')
        results = shard.get('results', {})
        require(set(results) == set(shard.get('selected', [])), 'Go shard completion differs from selection')
        require(all(status in ('pass', 'skip') for status in results.values()), 'Failed Go case')
        seen.extend(results)
    require(len(seen) == len(set(seen)) and set(seen) == set(names), 'Missing or duplicate Go execution')
    outcomes = {name: status for shard in server['shards'] for name, status in shard['results'].items()}
    for key,status in (('passed','pass'),('skipped','skip')):
        values=server.get(key,[])
        require(len(values)==len(set(values)) and set(values)=={name for name,result in outcomes.items() if result==status}, 'Mislabeled Go pass/skip receipt')


def browser(report, sha):
    require(report.get('status') == 'pass' and report.get('commit') == sha, 'Browser source/result mismatch')
    expected = {'-'.join(map(str, row)) for row in itertools.product(
        ('chromium', 'firefox', 'webkit'), ('zh-CN', 'en'), ('blue', 'comfort'), ('light', 'dark'), (1440, 390))}
    cases = report.get('cases', [])
    require(len(cases) == len(expected) and {row['id'] for row in cases} == expected, 'Missing browser matrix cells')
    require(all(row.get('status') == 'pass' and len(row.get('checks', [])) >= 5 for row in cases), 'Browser scenario incomplete')


def host(report, sha, name):
    require(report.get('status') == 'pass' and report.get('commit') == sha, 'Native host source/result mismatch')
    kind = 'windows' if name == 'windows' else 'macos'
    machine = {'windows': ('AMD64', 'x86_64'), 'macos-15': ('arm64', 'aarch64'), 'macos-15-intel': ('x86_64', 'AMD64')}[name]
    require(report.get('host') == kind and report.get('machine') in machine, 'Native host architecture mismatch')
    checks = report.get('checks', [])
    require(len(checks) == (10 if kind == 'windows' else 8) and all(c.get('status') == 'pass' for c in checks), 'Missing native host checks')
    required={'aide/cmd/aide-local-mounts','aide/internal/hostbridge','aide/internal/localmounts','aide/internal/messaging'}
    if kind=='windows':required.add('aide/scripts/xsys-review')
    tests=checks[0].get('goTests',[])
    require(required <= {event.get('Package') for event in tests if event.get('Action')=='pass' and event.get('Test')}, 'Native Go assertions missing or all skipped')


def collect(root, sha):
    result = {'status': 'pass', 'commit': sha, 'hosts': {}}
    for name in ('windows', 'macos-15', 'macos-15-intel'):
        report = json.loads((root / ('release-host-' + name) / 'report.json').read_text())
        host(report, sha, name)
        result['hosts'][name] = report
    report = json.loads((root / 'release-browser/report.json').read_text())
    browser(report, sha)
    result['browser'] = report
    launcher = json.loads((root / 'clean-linux-launcher-acceptance/result.json').read_text())
    require(launcher.get('status') == 'pass' and launcher.get('head') == sha, 'Clean Linux lifecycle mismatch')
    require(launcher.get('health_samples') and launcher.get('commands'), 'Missing real launcher lifecycle evidence')
    # Publish only a sanitized summary, not raw local paths, environment or logs.
    result['launcher'] = {key: launcher[key] for key in ('status', 'head', 'platform', 'health_samples')}
    return result
