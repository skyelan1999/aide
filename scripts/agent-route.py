#!/usr/bin/env python3
"""Local development workflow checks; never publishes or invokes a paid model."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import subprocess
import stat
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT).decode().strip()


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=path.name + '.', suffix='.tmp', dir=path.parent)
    temp = Path(temp_name)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            stream.write(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        temp.replace(path)
    finally:
        try:
            temp.unlink()
        except FileNotFoundError:
            pass


def create_json(path, data):
    """Reserve a JSON path atomically without following or replacing links/files."""
    path.parent.mkdir(parents=True, exist_ok=True)
    flags = os.O_CREAT | os.O_EXCL | os.O_WRONLY | getattr(os, 'O_NOFOLLOW', 0)
    fd = os.open(path, flags, 0o666)
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        stream.write(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
        stream.flush()
        os.fsync(stream.fileno())


def task_path(name):
    if not re.fullmatch(r'[a-z0-9]+(?:-[a-z0-9]+)*', name):
        raise ValueError('Task ID must contain lowercase letters, digits and hyphens')
    return ROOT / 'docs/tasks' / (name + '.json')


def check_timeout(command):
    # The owner must outlive the actual Docker host deadline and cleanup.
    # Read the runner's constants without executing its guarded main routine.
    if (command[:2] == ['python3', 'scripts/test_go_snapshot.py'] or
            command[:3] == ['bash', 'scripts/aide.sh', 'test']):
        runner = runpy.run_path(str(ROOT / 'scripts/test_go_snapshot.py'))
        return runner['HOST_TIMEOUT_SECONDS'] + 30
    return 600


def check_public_runtime_assets():
    try:
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/verify_prebuilt_assets.py'),
                                 str(ROOT / 'docker/prebuilt-images.tsv')], cwd=ROOT, timeout=45)
    except subprocess.TimeoutExpired as exc:
        raise ValueError('Public prebuilt asset verification timed out; release refused') from exc
    if result.returncode:
        raise ValueError('Public prebuilt assets are unavailable or do not match the default manifest')


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


def remove_candidate(root, path):
    """Unlink a cleanup candidate through directory handles, never path lookup."""
    relative = Path(path).relative_to(root)
    if (not relative.parts or not hasattr(os, 'O_NOFOLLOW') or os.open not in os.supports_dir_fd
            or os.unlink not in os.supports_dir_fd or os.stat not in os.supports_dir_fd
            or os.stat not in os.supports_follow_symlinks):
        raise OSError('Safe root-anchored cleanup is unavailable on this platform')
    flags = os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0) | os.O_NOFOLLOW
    root_fd = os.open(root, flags)
    parent_fd = root_fd
    opened = []
    try:
        for part in relative.parts[:-1]:
            parent_fd = os.open(part, flags, dir_fd=parent_fd)
            opened.append(parent_fd)
        info = os.stat(relative.parts[-1], dir_fd=parent_fd, follow_symlinks=False)
        if not stat.S_ISREG(info.st_mode):
            raise OSError('Cleanup candidate is not a regular file')
        os.unlink(relative.parts[-1], dir_fd=parent_fd)
    finally:
        for fd in reversed(opened):
            os.close(fd)
        os.close(root_fd)


def release_errors(task, receipt, config, current, current_head=None):
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
    if current_head is not None and receipt.get('head') != current_head:
        errors.append('The full verification receipt must match the current Git revision')
    return errors


def verification_passed(records, before, after):
    return bool(records) and all(record['exit'] == 0 for record in records) and before == after


def resolve_check_command(command):
    """Allow Windows hosts to bypass the WSL/Store command aliases explicitly."""
    overrides = {'bash': 'AIDE_BASH', 'python3': 'AIDE_PYTHON3'}
    setting = overrides.get(command[0])
    executable = os.environ.get(setting, '') if setting else ''
    if executable:
        path = Path(executable)
        if not path.is_file():
            raise ValueError(setting + ' is not a file: ' + executable)
        return [str(path), *command[1:]]
    return command


def select_skills(request, config):
    router = config['skill_router']
    text = request.casefold()
    matches = []
    for route in router['routes']:
        hits = []
        for word in route['keywords']:
            term = re.escape(word.casefold())
            if re.fullmatch(r'[a-z0-9_/.-]+', word.casefold()):
                matched = re.search(r'(?<![a-z0-9_])' + term + r'(?![a-z0-9_])', text)
            else:
                matched = word.casefold() in text
            if matched:
                hits.append(word)
        if hits:
            matches.append((len(hits), route))
    matches.sort(key=lambda item: (-item[0], item[1]['priority']))
    matches = [route for _, route in matches]
    return matches[:router['max_specialists']]


def render_route(request, config):
    router = config['skill_router']
    matches = select_skills(request, config)
    print('aide skill routing')
    print('Request: ' + request)
    print('Mode: ' + ('specialist matches' if matches else 'no keyword match; coordinator owns discovery'))
    print('Read the repository entry and shared workflow before assigning work. Routing is advisory; it does not spawn agents or grant authorization.')
    entry = ROOT / router['entry']
    print('\n--- ROUTER SKILL: ' + router['entry'] + ' ---\n' + entry.read_text())
    if not matches:
        print('\nSuggested owner: coordinator (use AGENTS.md and docs/agent/WORKFLOW.md; inspect before delegating).')
    for index, route in enumerate(matches):
        role = 'primary specialist' if index == 0 else 'optional collaborator; use only if this is a distinct concern'
        print('\n--- ' + role.upper() + ': ' + route['title'] + ' (' + route['id'] + ') ---')
        print('Skill: ' + route['skill'])
        print('Source entry points: ' + ', '.join(route['source_roots']))
        print('Verification suggestions: ' + '; '.join(route['verification']))
        print('Assignment: implement only this concern in its agreed file scope; report changed paths, actual commands/results, and unresolved items to the coordinator.')
        print('\n' + (ROOT / route['skill']).read_text())
    return matches


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='cmd', required=True)
    start = sub.add_parser('start'); start.add_argument('id'); start.add_argument('--request', required=True)
    prompt = sub.add_parser('prompt'); prompt.add_argument('client', choices=['codex', 'claude', 'deepseek', 'doubao', 'workbuddy']); prompt.add_argument('--request')
    route = sub.add_parser('route'); route.add_argument('--request', required=True)
    verify = sub.add_parser('verify'); verify.add_argument('profile', choices=['quick', 'full'])
    release = sub.add_parser('release-check'); release.add_argument('id')
    for name in ('check', 'audit', 'clean'):
        sub.add_parser(name)
    args = parser.parse_args()
    config = json.loads((ROOT / 'docs/agent/router.json').read_text())
    if args.cmd == 'start':
        path = task_path(args.id)
        data = {'id': args.id, 'request': args.request, 'baseline': git('rev-parse', 'HEAD'),
                'initial_status': git('status', '--short'), 'acceptance': [], 'scope': [],
                'stages': {s: {'status': 'pending', 'summary': '', 'evidence': []} for s in config['stages']},
                'release_authorization': '', 'rollback': '', 'handoff': ''}
        try:
            create_json(path, data)
        except FileExistsError as exc:
            raise ValueError('Task already exists; resume it, do not overwrite') from exc
        print(path.relative_to(ROOT))
    elif args.cmd == 'prompt':
        print('aide workflow v1 / client: ' + args.client)
        print('Read the following rules. Confirm task ID, stage, acceptance and unavailable capabilities.\n')
        for file in ('AGENTS.md', config['workflow']):
            print('\n--- ' + file + ' ---\n' + (ROOT / file).read_text())
        print('\nResume the applicable docs/tasks/<id>.json. Request its contents if local access is unavailable.')
        if args.request:
            print('\n--- SPECIALIST ROUTING ---')
            render_route(args.request, config)
    elif args.cmd == 'route':
        render_route(args.request, config)
    elif args.cmd in ('audit', 'clean'):
        for p in candidates(config):
            print(('DELETE ' if args.cmd == 'clean' else 'CANDIDATE ') + str(p.relative_to(ROOT)))
            if args.cmd == 'clean':
                remove_candidate(ROOT, p)
        print('Untracked files to classify (NOT deletion candidates):\n' + git('ls-files', '--others', '--exclude-standard'))
    elif args.cmd == 'check':
        missing = [f for f in config['required_files'] if not (ROOT / f).is_file()]
        router = config['skill_router']
        skill_files = [router['entry'], *(item['skill'] for item in router['routes'])]
        missing.extend(f for f in skill_files if not (ROOT / f).is_file())
        if missing:
            raise ValueError('Missing: ' + ', '.join(missing))
        print('PASS: route and specialist skill files present; client loading must still be confirmed')
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
                    # The race-enabled server suite takes longer than ten minutes
                    # on the supported Docker runner. Keep other checks bounded,
                    # but allow the full suite to finish and produce a real receipt.
                    timeout = check_timeout(command)
                    code = subprocess.run(resolve_check_command(command), cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout).returncode
                except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
                    stream.write(str(exc) + '\n'); code = 1
                records.append({'command': command, 'exit': code})
        passed = verification_passed(records, before, fingerprint())
        receipt = {'profile': args.profile, 'status': 'pass' if passed else 'fail', 'fingerprint': before,
                   'head': git('rev-parse', 'HEAD'), 'time': stamp, 'results': records, 'log': str(log.relative_to(ROOT))}
        write_json(ROOT / '.agent-state' / (args.profile + '.json'), receipt)
        print(json.dumps(receipt, ensure_ascii=False, indent=2))
        return 0 if passed else 1
    elif args.cmd == 'release-check':
        task = json.loads(task_path(args.id).read_text())
        rp = ROOT / '.agent-state/full.json'
        receipt = json.loads(rp.read_text()) if rp.exists() else {}
        errors = release_errors(task, receipt, config, fingerprint(), git('rev-parse', 'HEAD'))
        if git('branch', '--show-current') != 'main': errors.append('Release requires main')
        if git('status', '--porcelain'): errors.append('Release requires clean Git status')
        if errors: raise ValueError('\n'.join(errors))
        check_public_runtime_assets()
        print('PASS: local prerequisites and public default assets. No merge, tag, push or deployment performed.')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print('BLOCKED: ' + str(exc), file=sys.stderr)
        sys.exit(1)
