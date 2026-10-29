#!/usr/bin/env python3
"""Fail-closed identity/digest gates for Aide's existing release packager.

No build, tag creation, release publication, secret access, or plugin installation.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

from build_candidate import verification_plan

ROOT = Path(__file__).resolve().parents[2]


def capture(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for data in iter(lambda: stream.read(8 << 20), b''):
            h.update(data)
    return h.hexdigest()


def expected_tag():
    version = capture(['bash', 'scripts/version.sh', 'show'])
    require(re.fullmatch(r'\d+\.\d+\.\d+\.\d+ RC[1-9]\d*', version), 'Invalid repository version')
    return 'v' + version.replace(' RC', '-RC')


def check_weekly_cadence(releases, now=None):
    now = now or datetime.datetime.now(datetime.timezone.utc)
    require(all(r.get('published_at') for r in releases if not r.get('draft')), 'Published release metadata lacks timestamp')
    published = [datetime.datetime.fromisoformat(r['published_at'].replace('Z', '+00:00'))
                 for r in releases if not r.get('draft') and r.get('published_at')]
    if published:
        earliest = max(published) + datetime.timedelta(days=7)
        require(now >= earliest, 'Weekly release limit: next publication no earlier than ' + earliest.isoformat())


def cadence():
    repo = os.environ['GH_REPO']
    require(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo), 'Invalid repository')
    exception = os.environ.get('RELEASE_CADENCE_EXCEPTION_REASON', '').strip()
    if exception:
        event = os.environ.get('GH_EVENT_NAME', '')
        require(event == 'workflow_dispatch', 'Cadence exception is only allowed for a manual workflow dispatch')
        require(20 <= len(exception) <= 500 and not any(ord(c) < 32 and c not in '\t' for c in exception),
                'Cadence exception reason must be 20-500 printable characters')
        print('PASS: one-off weekly cadence exception; reason: ' + exception)
        return
    records = json.loads(capture(['gh', 'api', f'repos/{repo}/releases?per_page=100']))
    check_weekly_cadence(records)
    print('PASS: weekly public release cadence')


def release_notes(tag):
    path = ROOT / 'docs/releases' / (tag + '.md')
    require(path.is_file(), 'Missing reviewed human-readable release notes: ' + str(path))
    text = path.read_text().strip()
    require(text.splitlines()[0] == '# aide ' + tag, 'Release notes must name the exact release tag')
    for title in ('新增与改进', '修复', '升级影响', '已知问题'):
        require('## ' + title in text, 'Release notes missing section: ' + title)
    require(len(text) >= 150 and 'TODO' not in text, 'Release notes are incomplete')
    return text + '\n'


def source():
    sha = capture(['git', 'rev-parse', 'HEAD'])
    subprocess.run(['git', 'merge-base', '--is-ancestor', sha, 'origin/main'], cwd=ROOT, check=True)
    require(not capture(['git', 'status', '--porcelain']), 'Dirty checkout')
    tag = os.environ.get('RELEASE_TAG', '')
    exception = os.environ.get('RELEASE_CADENCE_EXCEPTION_REASON', '').strip()
    require(not exception or bool(tag), 'Cadence exception reason requires a release tag')
    if tag:
        require(tag == expected_tag(), 'Release tag must match scripts/version.sh')
        require(capture(['git', 'rev-parse', 'refs/tags/' + tag + '^{}']) == sha, 'Release tag must identify this main commit')
        release_notes(tag)
        cadence()
    output = f'sha={sha}\ntag={tag}\npublish={str(bool(tag)).lower()}\n'
    with open(os.environ['GITHUB_OUTPUT'], 'a') as stream:
        stream.write(output)
    print('PASS: trusted main commit; ' + ('release requested' if tag else 'candidate only'))


def validate_candidates(root, tag, sha):
    require(re.fullmatch(r'v\d+\.\d+\.\d+\.\d+-RC[1-9]\d*', tag), 'Invalid release tag')
    require(re.fullmatch(r'[0-9a-f]{40}', sha), 'Invalid release commit')
    expected = {}
    sources = set()
    for arch in ('amd64', 'arm64'):
        folder = root / ('aide-candidate-' + arch)
        proof = json.loads((folder / 'candidate-proof.json').read_text())
        require(proof.get('status') == 'pass' and proof.get('candidateOnly') is True and
                proof.get('published') is False and proof.get('pluginsBundled') is False, 'Unverified candidate')
        require((proof.get('tag'), proof.get('commit'), proof.get('platform')) == (tag, sha, 'linux/' + arch), 'Candidate identity mismatch')
        for key in ('smoke', 'privacy', 'packageValidation', 'importValidation'):
            require(proof.get(key, {}).get('status') == 'pass', 'Missing successful ' + key)
        image = f'aide:ci-{sha[:12]}-{arch}'
        require(proof.get('image') == image, 'Unexpected candidate image')
        plan = [{'command': command, 'environmentOverrides': env, 'status': 'pass'}
                for command, env in verification_plan(image)]
        require(proof.get('verification') == plan, 'Missing complete candidate verification plan')
        require(proof['privacy'].get('pluginPayloads') == 0 and proof['smoke'].get('installedPlugins') == 0, 'Candidate contains plugins')
        source_hash = proof.get('sourceSHA256', '')
        require(re.fullmatch(r'[0-9a-f]{64}', source_hash), 'Missing source digest')
        sources.add(source_hash)
        asset = proof['asset']
        name = f'aide-{tag}-full-linux-{arch}.zip'
        require(asset.get('name') == name, 'Unexpected artifact name')
        path = folder / name
        require(not path.is_symlink() and path.is_file(), 'Missing regular asset')
        require(path.stat().st_size == asset.get('bytes') and digest(path) == asset.get('sha256'), 'Asset size or digest mismatch')
        toolbox = proof.get('toolbox', {})
        require(toolbox.get('status') == 'pass' and toolbox.get('image') == image + '-test-tools', 'Missing verified SDK')
        require((toolbox.get('platform'), toolbox.get('sourceSHA256'), toolbox.get('commit')) ==
                ('linux/' + arch, source_hash, sha), 'SDK source/platform mismatch')
        for key in ('checks', 'privacy', 'importValidation', 'packageValidation'):
            require(toolbox.get(key, {}).get('status') == 'pass', 'Incomplete SDK verification: ' + key)
        require(toolbox['privacy'].get('pluginPayloads') == 0, 'SDK contains plugins')
        sdk = toolbox['asset']
        sdk_name = f'aide-{tag}-devtools-linux-{arch}.zip'
        sdk_path = folder / sdk_name
        require(sdk.get('name') == sdk_name and not sdk_path.is_symlink() and sdk_path.is_file(), 'Invalid SDK asset')
        require(sdk_path.stat().st_size == sdk.get('bytes') and digest(sdk_path) == sdk.get('sha256'), 'SDK asset size or digest mismatch')
        expected[sdk_name] = {'bytes': sdk_path.stat().st_size, 'sha256': sdk['sha256']}
        require({p.name for p in folder.iterdir()} == {name, sdk_name, 'candidate-proof.json'}, 'Unexpected candidate files')
        expected[name] = {'bytes': path.stat().st_size, 'sha256': asset['sha256']}
    require(len(sources) == 1, 'Architectures use different source inputs')
    return expected, sources.pop()


def artifacts(root):
    tag, sha = os.environ['RELEASE_TAG'], os.environ['RELEASE_SHA']
    require(tag == expected_tag() and capture(['git', 'rev-parse', 'HEAD']) == sha, 'Publisher checkout identity mismatch')
    expected, source_hash = validate_candidates(root, tag, sha)
    require(source_hash == capture(['bash', 'scripts/aide.sh', 'source-sha']), 'Candidate source differs from publisher checkout')
    notes = {'tag': tag, 'commit': sha, 'sourceSHA256': source_hash, 'pluginsBundled': False,
             'architectures': ['linux/arm64', 'linux/amd64'], 'assets': expected,
             'optionalDevelopmentTools': True, 'cadence': 'at most one public release every 7 days',
             'checks': 'Native build, full repository verification, isolated auth/runtime smoke, all image layers, package CRC and SHA256; Windows PowerShell 5.1 and 7 launcher fixtures.',
             'limits': 'No native Windows Docker Desktop or macOS host launch acceptance; no production deployment.'}
    (root / 'RELEASE-NOTES.md').write_text(release_notes(tag))
    (root / 'RELEASE-ASSETS.txt').write_text(json.dumps(notes, ensure_ascii=False, indent=2) + '\n')
    expected['RELEASE-ASSETS.txt'] = {'bytes': (root / 'RELEASE-ASSETS.txt').stat().st_size, 'sha256': digest(root / 'RELEASE-ASSETS.txt')}
    (root / 'SHA256SUMS').write_text(''.join(v['sha256'] + '  ' + name + '\n' for name, v in sorted(expected.items())))
    print('PASS: both native candidates match reviewed source and contain no plugins')


def remote(root):
    tag, sha, repo = os.environ['RELEASE_TAG'], os.environ['RELEASE_SHA'], os.environ['GH_REPO']
    expected, _ = validate_candidates(root, tag, sha)
    for name in ('SHA256SUMS', 'RELEASE-ASSETS.txt'):
        expected[name] = {'bytes': (root / name).stat().st_size, 'sha256': digest(root / name)}
    require(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo), 'Invalid repository')
    ref = json.loads(capture(['gh', 'api', f'repos/{repo}/git/ref/tags/{tag}']))['object']
    if ref['type'] == 'tag':
        ref = json.loads(capture(['gh', 'api', f'repos/{repo}/git/tags/' + ref['sha']]))['object']
    require(ref['type'] == 'commit' and ref['sha'] == sha, 'Remote tag moved')
    # The tag endpoint exposes published releases, not a newly created draft.
    # gh release view resolves drafts for the authenticated publisher; then pin
    # the API read to the exact database ID and verify its tag before using assets.
    identity = json.loads(capture(['gh', 'release', 'view', tag, '--repo', repo, '--json', 'databaseId']))
    release_id = identity.get('databaseId')
    require(type(release_id) is int and release_id > 0, 'Invalid release database ID')
    release = json.loads(capture(['gh', 'api', f'repos/{repo}/releases/{release_id}']))
    require(release.get('id') == release_id and release.get('tag_name') == tag, 'Remote release identity mismatch')
    require(release['draft'] is True, 'Expected an unpublished draft')
    require(release.get('body', '').strip() == (root / 'RELEASE-NOTES.md').read_text().strip(), 'Remote release notes mismatch')
    assets = {a['name']: a for a in release['assets']}
    require(set(assets) == set(expected), 'Unexpected or missing remote assets')
    for name, value in expected.items():
        require(assets[name]['size'] == value['bytes'] and assets[name].get('digest') == 'sha256:' + value['sha256'], 'Independent remote digest mismatch: ' + name)
    print('PASS: exact remote asset set, SHA256 digests and immutable source identity')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['source', 'artifacts', 'remote', 'cadence'])
    parser.add_argument('--root', type=Path, default=ROOT / '.agent-state/delivery')
    args = parser.parse_args()
    if args.mode in ('source', 'cadence'):
        globals()[args.mode]()
    else:
        globals()[args.mode](args.root)


if __name__ == '__main__':
    main()
