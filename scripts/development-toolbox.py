#!/usr/bin/env python3
"""Optional Aide SDK: build once, export for offline load, run explicit commands.

Never starts/replaces an Aide service or mounts its Docker socket/data volumes.
"""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import subprocess
import uuid

ROOT = Path(__file__).resolve().parents[1]


def inspect(image, profile):
    value = json.loads(subprocess.check_output(['docker', 'image', 'inspect', image], text=True))[0]
    if value['Os'] != 'linux' or value['Architecture'] not in ('arm64', 'amd64'):
        raise ValueError('Expected a Linux ARM64/AMD64 image')
    if value['Config'].get('Labels', {}).get('aide.runtime-profile') != profile:
        raise ValueError('Expected image profile: ' + profile)
    return value


def run_command(image_id, workspace, command, network=False):
    workspace = workspace.resolve(strict=True)
    if not workspace.is_dir() or ',' in str(workspace):
        raise ValueError('Workspace must be a directory without commas')
    if not command:
        raise ValueError('Supply a command after --')
    return ['docker', 'run', '--rm', '--init', '--name', 'aide-toolbox-' + uuid.uuid4().hex[:12],
            '--network', 'bridge' if network else 'none', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges', '--memory', '2g', '--cpus', '2',
            '--pids-limit', '256', '--read-only', '--tmpfs', '/tmp:rw,exec,size=1g',
            '--tmpfs', '/home/aide:rw,exec,uid=1000,gid=1000,size=1g',
            '--mount', 'type=bind,src=' + str(workspace) + ',dst=/workspace',
            '--workdir', '/workspace', '--user', '1000:1000',
            '-e', 'HOME=/home/aide', '-e', 'GOCACHE=/tmp/go-build', '-e', 'GOPATH=/tmp/go',
            '--entrypoint', command[0], image_id, *command[1:]]


def export_image(info, output):
    output = output.resolve()
    # Exclusive files avoid clobbering existing exports, even after a failure.
    checksum = Path(str(output) + '.sha256')
    if output.exists() or checksum.exists():
        raise ValueError('Export or checksum already exists; choose a new output path')
    output.parent.mkdir(parents=True, exist_ok=True)
    created = False
    try:
        with output.open('xb') as stream:
            created = True
            with gzip.GzipFile(fileobj=stream, mode='wb', compresslevel=1, mtime=0) as zipped:
                with subprocess.Popen(['docker', 'image', 'save', info['Id']], stdout=subprocess.PIPE) as process:
                    while block := process.stdout.read(8 << 20):
                        zipped.write(block)
                    if process.wait() != 0:
                        raise RuntimeError('Docker image export failed')
        digest = hashlib.sha256()
        with output.open('rb') as stream:
            while block := stream.read(8 << 20):
                digest.update(block)
        with checksum.open('x') as stream:
            stream.write(digest.hexdigest() + '  ' + output.name + '\n')
    except BaseException:
        if created:
            output.unlink(missing_ok=True)
        raise
    return {'imageId': info['Id'], 'platform': info['Os'] + '/' + info['Architecture'],
            'archive': str(output), 'bytes': output.stat().st_size, 'sha256': digest.hexdigest()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action', required=True)
    build = commands.add_parser('build', help='publisher/developer machine; needs network/base caches')
    build.add_argument('--runtime', required=True)
    build.add_argument('--image', required=True)
    export = commands.add_parser('export', help='export only toolbox layers, without user state')
    export.add_argument('--image', required=True)
    export.add_argument('--output', type=Path, required=True)
    execute = commands.add_parser('run', help='run an explicit command against one workspace')
    execute.add_argument('--image', required=True)
    execute.add_argument('--workspace', type=Path, required=True)
    execute.add_argument('--network', action='store_true', help='allow package downloads (default: none)')
    execute.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.action == 'build':
        runtime = inspect(args.runtime, 'slim')
        # A local tag avoids registry resolution of a bare sha256 in FROM.
        seed = 'aide:sdk-seed-' + runtime['Id'].removeprefix('sha256:')
        subprocess.run(['docker', 'image', 'tag', runtime['Id'], seed], check=True)
        subprocess.run(['docker', 'build', '--platform', 'linux/' + runtime['Architecture'],
                        '-f', str(ROOT / 'docker/Dockerfile.sdk'), '--build-arg',
                        'AIDE_RUNTIME_IMAGE=' + seed, '-t', args.image, str(ROOT)], check=True)
        inspect(args.image, 'toolbox')
    elif args.action == 'export':
        print(json.dumps(export_image(inspect(args.image, 'toolbox'), args.output)))
    else:
        command = args.command[1:] if args.command[:1] == ['--'] else args.command
        subprocess.run(run_command(inspect(args.image, 'toolbox')['Id'], args.workspace,
                                   command, args.network), check=True)


if __name__ == '__main__':
    main()
