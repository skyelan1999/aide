#!/usr/bin/env python3
"""Local development workflow checks; never publishes or invokes a paid model."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT).decode().strip()


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix('.tmp')
    temp.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
    temp.replace(path)


def task_path(name):
    if not re.fullmatch(r'[a-z0-9]+(?:-[a-z0-9]+)*', name):
        raise ValueError('Task ID must contain lowercase letters, digits and hyphens')
    return ROOT / 'docs/tasks' / (name + '.json')


def fingerprint():
    names = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT).split(b'\0')
    digest = hashlib.sha256()
    for raw in sorted(set(filter(None, names))):
        name = os.fsdecode(raw)
        if (name.startswith('docs/') and not name.startswith('docs/agent/')) or name == 'version.md':
            continue
        p = ROOT / name
        digest.update(raw + b'\0')
        digest.update(os.readlink(p).encode() if p.is_symlink() else p.read_bytes() if p.is_file() else b'<deleted>')
    return digest.hexdigest()


def candidates(config):
    protected = set(config['cleanup']['protected_roots'])
    tracked = set(git('ls-files', '-z').split('\0'))
    result = []
    for base, dirs, files in os.walk(ROOT, followlinks=False):
        dirs[:] = [d for d in dirs if d not in protected and not (Path(base) / d).is_symlink()]
        for name in files:
            p = Path(base) / name
            if name == '.DS_Store' and not p.is_symlink() and str(p.relative_to(ROOT)) not in tracked:
                result.append(p)
    return result


def release_errors(task, receipt, config, current):
    errors = []
    for field in ('request', 'acceptance', 'scope', 'release_authorization', 'rollback'):
        if not task.get(field):
            errors.append('Missing ' + field)
    for stage in config['stages'][:-1]:
        record = task.get('stages', {}).get(stage, {})
        if record.get('status') != 'pass' or not record.get('summary') or not record.get('evidence'):
            errors.append('Incomplete stage: ' + stage)
    if receipt.get('profile') != 'full' or receipt.get('status') != 'pass' or receipt.get('fingerprint') != current:
        errors.append('A passing full verification of current inputs is required')
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='cmd', required=True)
    start = sub.add_parser('start'); start.add_argument('id'); start.add_argument('--request', required=True)
    prompt = sub.add_parser('prompt'); prompt.add_argument('client', choices=['codex', 'claude', 'deepseek', 'doubao', 'workbuddy'])
    verify = sub.add_parser('verify'); verify.add_argument('profile', choices=['quick', 'full'])
    release = sub.add_parser('release-check'); release.add_argument('id')
    for name in ('check', 'audit', 'clean'):
        sub.add_parser(name)
    args = parser.parse_args()
    config = json.loads((ROOT / 'docs/agent/router.json').read_text())
    if args.cmd == 'start':
        path = task_path(args.id)
        if path.exists():
            raise ValueError('Task already exists; resume it, do not overwrite')
        write_json(path, {'id': args.id, 'request': args.request, 'baseline': git('rev-parse', 'HEAD'),
                         'initial_status': git('status', '--short'), 'acceptance': [], 'scope': [],
                         'stages': {s: {'status': 'pending', 'summary': '', 'evidence': []} for s in config['stages']},
                         'release_authorization': '', 'rollback': '', 'handoff': ''})
        print(path.relative_to(ROOT))
    elif args.cmd == 'prompt':
        print('aide workflow v1 / client: ' + args.client)
        print('Read the following rules. Confirm task ID, stage, acceptance and unavailable capabilities.\n')
        for file in ('AGENTS.md', config['workflow']):
            print('\n--- ' + file + ' ---\n' + (ROOT / file).read_text())
        print('\nResume the applicable docs/tasks/<id>.json. Request its contents if local access is unavailable.')
    elif args.cmd in ('audit', 'clean'):
        for p in candidates(config):
            print(('DELETE ' if args.cmd == 'clean' else 'CANDIDATE ') + str(p.relative_to(ROOT)))
            if args.cmd == 'clean':
                p.unlink()
        print('Untracked files to classify (NOT deletion candidates):\n' + git('ls-files', '--others', '--exclude-standard'))
    elif args.cmd == 'check':
        missing = [f for f in config['required_files'] if not (ROOT / f).is_file()]
        if missing:
            raise ValueError('Missing: ' + ', '.join(missing))
        print('PASS: route files present; client loading must still be confirmed')
    elif args.cmd == 'verify':
        before = fingerprint()
        records = []
        checks = config['checks']['quick'] + (config['checks']['full'] if args.profile == 'full' else [])
        stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
        log = ROOT / '.agent-state' / ('verify-' + stamp + '.log')
        log.parent.mkdir(exist_ok=True)
        with log.open('w') as stream:
            for command in checks:
                print('RUN ' + ' '.join(command), flush=True)
                stream.write('\n$ ' + ' '.join(command) + '\n'); stream.flush()
                try:
                    code = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT, timeout=600).returncode
                except (OSError, subprocess.TimeoutExpired) as exc:
                    stream.write(str(exc) + '\n'); code = 1
                records.append({'command': command, 'exit': code})
        passed = all(r['exit'] == 0 for r in records) and before == fingerprint()
        receipt = {'profile': args.profile, 'status': 'pass' if passed else 'fail', 'fingerprint': before,
                   'head': git('rev-parse', 'HEAD'), 'time': stamp, 'results': records, 'log': str(log.relative_to(ROOT))}
        write_json(ROOT / '.agent-state' / (args.profile + '.json'), receipt)
        print(json.dumps(receipt, ensure_ascii=False, indent=2))
        return 0 if passed else 1
    elif args.cmd == 'release-check':
        task = json.loads(task_path(args.id).read_text())
        rp = ROOT / '.agent-state/full.json'
        receipt = json.loads(rp.read_text()) if rp.exists() else {}
        errors = release_errors(task, receipt, config, fingerprint())
        if git('branch', '--show-current') != 'main': errors.append('Release requires main')
        if git('status', '--porcelain'): errors.append('Release requires clean Git status')
        if errors: raise ValueError('\n'.join(errors))
        print('PASS: local release prerequisites. No merge, tag, push or deployment performed.')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print('BLOCKED: ' + str(exc), file=sys.stderr)
        sys.exit(1)
