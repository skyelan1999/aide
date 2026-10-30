#!/usr/bin/env python3
"""Explicit source build, scoped Docker cleanup, diagnostics and delivery (#640).

POSIX entrypoints use this coordinator. Windows entrypoints use the native C++
host; no host Python or PowerShell is required there. Never executes .env.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
IMAGE_ID = re.compile(r"sha256:[a-f0-9]{64}\Z")
CONTAINER_ID = re.compile(r"[a-f0-9]{12,64}\Z")


def source_package(root=ROOT):
    return (root / '.git').exists() or ((root / 'go.mod').is_file() and (root / 'Dockerfile').is_file())


def version(root=ROOT):
    text = (root / 'version.md').read_text() if (root / 'version.md').is_file() else ''
    match = re.search(r'\b(\d+\.\d+\.\d+\.\d+) RC([1-9]\d*)\b', text)
    if match:
        return match[0], 'v' + match[1] + '-RC' + match[2]
    if (root / '.aide-image').is_file():
        fields = (root / '.aide-image').read_text().split()
        if len(fields) == 4 and re.fullmatch(r'v\d+\.\d+\.\d+\.\d+-RC[1-9]\d*', fields[3]):
            return fields[3][1:].replace('-RC', ' RC'), fields[3]
    raise ValueError('Version identity missing; restore version.md or the complete Release package')


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink() or path.parent.is_symlink():
        raise ValueError('Launcher state must not use symlinks')
    with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, delete=False) as out:
        temporary = Path(out.name)
        try:
            json.dump(value, out, indent=2)
            out.write('\n')
            out.close()
            os.replace(temporary, path)
        finally:
            temporary.unlink(missing_ok=True)


class Launcher:
    def __init__(self, root=ROOT, dry_run=False):
        self.root = root.resolve()
        self.dry_run = dry_run
        self.scope = hashlib.sha256(os.fsencode(str(self.root))).hexdigest()[:20]
        self.builder = 'aide-' + self.scope
        self.image = 'aide:source-' + self.scope
        self.state = self.root / '.aide-data/build'
        self.receipt = self.state / 'owner.json'
        self.docker = shutil.which('docker') or str(Path.home() / '.docker/bin/docker')

    def call(self, args, capture=False, check=True, timeout=180, env=None):
        result = subprocess.run(args, cwd=self.root, env=env, text=True,
                                stdout=subprocess.PIPE if capture else None,
                                stderr=subprocess.PIPE if capture else None,
                                timeout=timeout, check=False)
        if check and result.returncode:
            raise RuntimeError(f'{Path(args[0]).name} {args[1] if len(args) > 1 else ""} failed ({result.returncode}); see local launcher output')
        return result.stdout.strip() if capture and result.returncode == 0 else result

    def d(self, *args, **kwargs):
        return self.call([self.docker, *args], **kwargs)

    def require_source(self):
        if not source_package(self.root):
            raise ValueError('This is a Release package: build, clean and release are source-package commands')

    def validate_state(self):
        for path in (self.root / '.aide-data', self.state, self.receipt):
            if path.is_symlink():
                raise ValueError('Build state must not use symbolic links')

    def owner(self):
        self.validate_state()
        if not self.receipt.exists():
            return None
        if self.receipt.is_symlink() or self.state.is_symlink():
            raise ValueError('Build ownership receipt must not use symlinks')
        value = json.loads(self.receipt.read_text())
        if value != {'version': 1, 'scope': self.scope, 'builder': self.builder}:
            raise ValueError('Build ownership receipt does not match this installation')
        return value

    def ensure_builder(self):
        self.validate_state()
        existing = self.d('buildx', 'inspect', self.builder, capture=True, check=False)
        if isinstance(existing, str):
            if not self.owner():
                raise ValueError('A builder with this name exists without this installation receipt; refusing to adopt it')
            return
        self.d('buildx', 'create', '--name', self.builder, '--driver', 'docker-container', '--driver-opt', 'memory=4g,memory-swap=4g,cpu-period=100000,cpu-quota=200000,restart-policy=no')
        atomic_json(self.receipt, {'version': 1, 'scope': self.scope, 'builder': self.builder})

    def configure_image(self):
        path = self.root / '.env'
        if path.is_symlink():
            raise ValueError('.env must not be a symbolic link')
        text = path.read_text() if path.exists() else (self.root / '.env.example').read_text()
        for key, value in {'AIDE_IMAGE': self.image, 'AIDE_INSTALLATION_KIND': 'source'}.items():
            line = key + '=' + value
            pattern = re.compile(r'^' + key + r'=.*$', re.M)
            text = pattern.sub(line, text) if pattern.search(text) else text.rstrip() + '\n' + line + '\n'
        with tempfile.NamedTemporaryFile(mode='w', dir=self.root, prefix='.env-build-', delete=False) as out:
            temporary = Path(out.name)
            try:
                out.write(text)
                out.close()
                os.replace(temporary, path)
            finally:
                temporary.unlink(missing_ok=True)

    def build(self):
        self.require_source()
        if self.dry_run:
            print(json.dumps({'action': 'build', 'builder': self.builder, 'image': self.image,
                              'steps': ['compile current source and embedded host components', 'verify image identity', 'start']}, indent=2))
            return
        self.d('info', capture=True)
        self.call(['bash', '-ec', 'source scripts/resource-guard.sh; aide_check_disk build'])
        source = self.call(['bash', 'scripts/aide.sh', 'source-sha'], capture=True)
        if not re.fullmatch(r'[a-f0-9]{64}', source):
            raise ValueError('Invalid current source fingerprint')
        display, _ = version(self.root)
        commit = self.call(['git', 'rev-parse', 'HEAD'], capture=True)
        if self.call(['git', 'status', '--porcelain'], capture=True):
            commit += '-dirty'
        configuration = json.loads(self.d('compose', 'config', '--format', 'json', capture=True))
        build = configuration['services']['aide']['build']
        arguments = dict(build.get('args') or {})
        arguments.update(AIDE_VERSION=display, AIDE_COMMIT=commit, AIDE_SRC_SHA=source, AIDE_RUNTIME_MODE='source')
        self.ensure_builder()
        command = ['buildx', 'build', '--builder', self.builder, '--load', '--pull=false',
                   '--label', 'aide.installation=' + self.scope, '--label', 'aide.build-role=runtime',
                   '--file', build.get('dockerfile', 'Dockerfile'), '--tag', self.image]
        if build.get('network'):
            command += ['--network', build['network']]
        for key, value in sorted(arguments.items()):
            if value is not None:
                command += ['--build-arg', key + '=' + str(value)]
        self.d(*command, '.', timeout=7200)
        metadata = json.loads(self.d('image', 'inspect', self.image, '--format', '{{json .}}', capture=True))
        labels = metadata.get('Config', {}).get('Labels', {})
        if (not IMAGE_ID.fullmatch(metadata.get('Id', '')) or metadata.get('Os') != 'linux'
                or metadata.get('Architecture') not in ('amd64', 'arm64')
                or labels.get('aide.runtime-profile') != 'slim'
                or labels.get('aide.srcsha') != source or labels.get('aide.installation') != self.scope):
            raise ValueError('Built image identity/source/installation did not verify; existing startup configuration retained')
        self.configure_image()
        print('Source build verified; starting this installation from ' + self.image, flush=True)
        env = dict(os.environ, AIDE_IMAGE=self.image, AIDE_INSTALLATION_KIND='source')
        self.call(['bash', 'scripts/aide.sh', 'start-built'], env=env, timeout=240)

    def owned_containers(self):
        ids = self.d('ps', '-aq', '--no-trunc', '--filter', 'label=com.docker.compose.project.working_dir=' + str(self.root), capture=True)
        result = []
        for cid in ids.split():
            if not CONTAINER_ID.fullmatch(cid):
                raise ValueError('Invalid owned container identity')
            value = json.loads(self.d('inspect', cid, '--format', '{{json .}}', capture=True))
            labels = value.get('Config', {}).get('Labels', {})
            owner = labels.get('com.docker.compose.project.working_dir', '')
            if not owner or Path(owner).resolve() != self.root:
                raise ValueError('Container belongs to another installation; cleanup refused')
            result.append((cid, value))
        return result

    def docker_clean(self):
        self.d('info', capture=True)
        containers = self.owned_containers()
        self.owner()  # Validate ownership before the first mutation.
        projects = {v['Config']['Labels'].get('com.docker.compose.project') for _, v in containers}
        projects.discard(None)
        # A shared Compose project is not permission to remove a foreign root.
        for project in projects:
            for cid in self.d('ps', '-aq', '--no-trunc', '--filter', 'label=com.docker.compose.project=' + project, capture=True).split():
                if cid not in {c for c, _ in containers}:
                    raise ValueError('Compose project includes a foreign installation; cleanup refused')
        images = {v.get('Image', '') for _, v in containers}
        for image in self.d('image', 'ls', '-q', '--no-trunc', '--filter', 'label=aide.installation=' + self.scope, capture=True).split():
            images.add(image)
        if any(not IMAGE_ID.fullmatch(image) for image in images):
            raise ValueError('Invalid owned image identity')
        if self.dry_run:
            print(json.dumps({'action': 'docker_clean', 'containers': [c for c, _ in containers],
                              'candidateImages': sorted(images), 'builder': self.builder,
                              'preserve': ['persistent data', 'workspace', 'settings', 'offline archives', 'shared images']}, indent=2))
            return
        self.call(['bash', 'scripts/aide-maintenance.sh', 'stop'], timeout=180)
        for cid, _ in containers:
            self.d('container', 'rm', cid)
        for project in sorted(projects):
            networks = self.d('network', 'ls', '-q', '--filter', 'label=com.docker.compose.project=' + project, capture=True)
            for network in networks.split():
                self.d('network', 'rm', network)
        for image in sorted(images):
            users = self.d('ps', '-aq', '--filter', 'ancestor=' + image, capture=True)
            if users:
                print('Shared image retained: ' + image)
                continue
            result = self.d('image', 'rm', image, check=False)
            if result.returncode:
                print('Image has other references; retained: ' + image)
        if self.owner():
            found = self.d('buildx', 'inspect', self.builder, capture=True, check=False)
            if isinstance(found, str):
                self.d('buildx', 'rm', self.builder, timeout=180)
            self.receipt.unlink()
        print('Owned Docker environment removed; persistent data and offline packages retained.')

    def verify(self):
        report = {'package': 'source' if source_package(self.root) else 'release', 'checks': {}}
        try:
            report['version'], report['tag'] = version(self.root)
        except ValueError as error:
            report['checks']['version'] = str(error)
        dependencies = [('docker', [self.docker, 'info']), ('compose', [self.docker, 'compose', 'version']),
                        ('configuration', [self.docker, 'compose', 'config', '--quiet'])]
        if report['package'] == 'source':
            dependencies += [('git', ['git', '--version']), ('buildx', [self.docker, 'buildx', 'version'])]
        for name, args in dependencies:
            try:
                result = self.call(args, capture=True, check=False, timeout=20)
            except (OSError, subprocess.SubprocessError):
                result = None
            report['checks'][name] = 'pass' if isinstance(result, str) else 'unavailable'
        logs = sorted((self.root / '.agent-state/startup').glob('start-*.log'))
        report['startupLog'] = str(logs[-1].relative_to(self.root)) if logs else None
        report['checks']['runtime'] = 'not running'
        if report['checks']['docker'] == 'pass' and report['checks']['configuration'] == 'pass':
            cid = self.d('compose', 'ps', '--status', 'running', '-q', 'aide', capture=True, check=False)
            if isinstance(cid, str) and CONTAINER_ID.fullmatch(cid):
                health = self.d('exec', cid, 'curl', '-fsSk', '--connect-timeout', '2', '--max-time', '3', 'https://127.0.0.1:8080/healthz', capture=True, check=False)
                try:
                    body = json.loads(health) if isinstance(health, str) else {}
                    report['checks']['runtime'] = 'pass' if body.get('service') == 'aide' and body.get('status') == 'ok' else 'unhealthy'
                except ValueError:
                    report['checks']['runtime'] = 'unhealthy'
        report['buildRequired'] = report['package'] == 'source' and report['checks']['runtime'] != 'pass'
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 0 if all(value == 'pass' for value in report['checks'].values()) and 'tag' in report else 1

    def release(self):
        self.require_source()
        display, tag = version(self.root)
        if self.call(['git', 'branch', '--show-current'], capture=True) != 'main' or self.call(['git', 'status', '--porcelain'], capture=True):
            raise ValueError('release requires a clean main checkout; use the existing version.sh workflow first')
        head = self.call(['git', 'rev-parse', 'HEAD'], capture=True)
        tagged = self.call(['git', 'rev-parse', 'refs/tags/' + tag + '^{}'], capture=True)
        if head != tagged:
            raise ValueError('The existing version.sh tag must identify this main commit')
        sys.path.insert(0, str(self.root / 'scripts/ci'))
        from delivery_gate import release_notes
        release_notes(tag)
        command = ['gh', 'workflow', 'run', 'aide-delivery.yml', '--repo', 'skyelan1999/aide', '--ref', 'main', '-f', 'release_tag=' + tag]
        if self.dry_run:
            print(json.dumps({'action': 'release', 'version': display, 'tag': tag, 'commit': head,
                              'notes': 'docs/releases/' + tag + '.md', 'command': command}, indent=2))
            return
        remote = self.call(['git', 'ls-remote', 'origin', 'refs/heads/main', 'refs/tags/' + tag + '^{}'], capture=True)
        refs = dict(line.split()[::-1] for line in remote.splitlines())
        if refs.get('refs/heads/main') != head or refs.get('refs/tags/' + tag + '^{}') != head:
            raise ValueError('Remote main and the annotated release tag must both match the current commit')
        checks = json.loads(self.call(['gh', 'api', 'repos/skyelan1999/aide/commits/' + head + '/check-runs'], capture=True))
        if not any(c.get('name') == 'gate' and c.get('status') == 'completed' and c.get('conclusion') == 'success' for c in checks.get('check_runs', [])):
            raise ValueError('The current main commit has no successful required gate; release was not dispatched')
        self.call(command)
        print('Delivery dispatched for ' + tag + '; native builds, notes, cadence and environment approval still run in the existing workflow. Dispatch is not publication.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('build', 'clean', 'docker_clean', 'verify', 'release'))
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    launcher = Launcher(dry_run=args.dry_run)
    try:
        if args.action == 'clean':
            launcher.require_source()
            launcher.docker_clean()
            launcher.build()
        else:
            return getattr(launcher, args.action)() or 0
        return 0
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
