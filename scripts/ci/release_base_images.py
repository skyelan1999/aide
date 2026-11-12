"""Reviewed release inputs; local development keeps its existing offline tags."""
import json
import re


def build_args(root, sdk=False):
    manifest = json.loads((root / 'docker/release-base-images.json').read_text())['images']
    args = []
    for name in (('NODE_BASE', 'GO_BASE') if sdk else ('PYTHON_BASE', 'NODE_BASE', 'GO_BASE')):
        pin = manifest[name]
        repository = {'PYTHON_BASE': 'python', 'NODE_BASE': 'node', 'GO_BASE': 'golang'}[name]
        if not re.fullmatch(repository + r':[a-zA-Z0-9_.-]+@sha256:[0-9a-f]{64}', pin['ref']):
            raise ValueError('Release base must use an official immutable digest: ' + name)
        for arch in ('arm64', 'amd64'):
            if not re.fullmatch(r'sha256:[0-9a-f]{64}', pin['platforms'].get(arch, '')):
                raise ValueError('Release base lacks required platform: ' + arch)
        args.extend(['--build-arg', name + '=' + pin['ref']])
    return args
