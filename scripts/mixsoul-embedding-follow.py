#!/usr/bin/env python3
"""Optional operator-side vector-service supervisor; never mounts Docker into Aide."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import sqlite3
import subprocess
import sys
import time
from urllib.parse import urlsplit

OWNER = 'aide.embedding-owner'


class Docker:
    def __init__(self, executable, context):
        self.command = [executable, '--context', context]

    def run(self, *args, environment=None):
        return subprocess.check_output(self.command + list(args), env=environment,
                                       stderr=subprocess.DEVNULL, timeout=30)

    def inspect(self, name):
        names = self.run('ps', '-a', '--format', '{{.Names}}').decode().splitlines()
        return json.loads(self.run('inspect', name))[0] if name in names else None


def configuration(main, project, port):
    labels = main['Config'].get('Labels') or {}
    if labels.get('com.docker.compose.project') != project or labels.get('com.docker.compose.service') != 'aide':
        raise ValueError('Main container does not belong to the selected Compose project')
    env = dict(e.split('=', 1) for e in main['Config'].get('Env', []) if '=' in e)
    if env.get('AIDE_ASSISTANT_ENGINE') != 'mixsoul':
        raise ValueError('MIXSoul is not enabled')
    cfg = json.loads(env.get('AIDE_MIXSOUL_LOCAL_EMBEDDING', '{}'))
    endpoint = urlsplit(cfg.get('endpoint', ''))
    if (endpoint.scheme != 'http' or endpoint.hostname not in ('127.0.0.1', '::1')
            or endpoint.port != port or endpoint.path != '/v1/embeddings'
            or endpoint.username or endpoint.password or endpoint.query or endpoint.fragment):
        raise ValueError('A matching loopback-only embedding endpoint is required')
    if type(cfg.get('dimensions')) is not int or cfg['dimensions'] != 384:
        raise ValueError('Unsupported vector dimensions')
    if cfg.get('model') != 'sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2':
        raise ValueError('The bundled vector service model is required')
    token = cfg.get('token', '')
    if not isinstance(token, str) or not re.fullmatch('[a-f0-9]{64}', token):
        raise ValueError('Invalid embedding credential')
    return cfg


def reconcile(docker, container, name, models, image, project, owner, port=32141):
    main = docker.inspect(container)
    if not main or not main['State']['Running']:
        return 'main-stopped'
    cfg = configuration(main, project, port)
    binding = hashlib.sha256(json.dumps([main['Id'], main['State']['StartedAt'], cfg,
                                        image, str(models)], sort_keys=True).encode()).hexdigest()
    old = docker.inspect(name)
    if old:
        labels = old['Config'].get('Labels') or {}
        if labels.get(OWNER) != owner or labels.get('aide.embedding-main') != container:
            raise ValueError('Refusing to replace an unrelated sidecar')
        if old['State']['Running'] and labels.get('aide.embedding-binding') == binding:
            if (docker.run('exec', container, 'readlink', '/proc/self/ns/net').strip()
                    == docker.run('exec', name, 'readlink', '/proc/self/ns/net').strip()):
                return 'ready'
    current = docker.inspect(container)
    if (not current or not current['State']['Running'] or current['Id'] != main['Id']
            or current['State']['StartedAt'] != main['State']['StartedAt']):
        return 'main-changing'
    if old:
        docker.run('rm', '-f', name)
    environment = dict(os.environ, MIXSOUL_EMBEDDING_TOKEN=cfg.get('token', ''))
    docker.run('run', '-d', '--name', name, '--pull', 'never', '--network', 'container:' + main['Id'],
               '--restart', 'unless-stopped', '--read-only', '--tmpfs', '/tmp:rw,noexec,nosuid,size=64m',
               '--memory', '2g', '--cpus', '2', '--pids-limit', '128', '--cap-drop', 'ALL',
               '--security-opt', 'no-new-privileges', '--label', OWNER + '=' + owner,
               '--label', 'aide.embedding-main=' + container, '--label', 'aide.embedding-binding=' + binding,
               '--mount', 'type=bind,src=' + str(models) + ',dst=/models,readonly',
               '-e', 'MIXSOUL_EMBEDDING_TOKEN', image, environment=environment)
    return 'rebound'


def main():
    os.umask(0o077)
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--docker', default='docker')
    p.add_argument('--context')
    p.add_argument('--project', required=True)
    p.add_argument('--container', required=True)
    p.add_argument('--name', required=True)
    p.add_argument('--image', required=True, help='Existing local image; never downloaded by this helper')
    p.add_argument('--models', type=Path, required=True)
    p.add_argument('--state', type=Path, default=Path('.aide-data/mixsoul-embedding-follow'))
    p.add_argument('--port', type=int, default=32141)
    p.add_argument('--once', action='store_true')
    args = p.parse_args()
    if not args.models.is_dir() or not 1 <= args.port <= 65535:
        raise ValueError('Existing local model directory and valid loopback port required')
    if not all(re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', value)
               for value in (args.project, args.container, args.name)):
        raise ValueError('Invalid project or container name')
    if not args.context:
        args.context = subprocess.check_output([args.docker, 'context', 'show'], timeout=10).decode().strip()
    args.state.mkdir(parents=True, exist_ok=True, mode=0o700)
    owner = hashlib.sha256(json.dumps([args.context, args.project, args.container,
                                      str(args.state.resolve())]).encode()).hexdigest()
    # SQLite releases the cross-platform OS lock even after a crashed supervisor.
    with sqlite3.connect(args.state / 'supervisor.sqlite', timeout=0) as lock:
        lock.execute('BEGIN EXCLUSIVE')
        docker = Docker(args.docker, args.context)
        stopped = False
        def stop(*_):
            nonlocal stopped
            stopped = True
        signal.signal(signal.SIGINT, stop)
        signal.signal(signal.SIGTERM, stop)
        previous = None
        while not stopped:
            try:
                status = reconcile(docker, args.container, args.name, args.models.resolve(),
                                   args.image, args.project, owner, args.port)
            except Exception as error:
                if args.once:
                    raise
                status = 'retry:' + type(error).__name__
            if status != previous:
                print(time.strftime('%Y-%m-%dT%H:%M:%S%z'), status, flush=True)
                previous = status
            if args.once:
                return
            until = time.monotonic() + (15 if status.startswith('retry:') else 3)
            while not stopped and time.monotonic() < until:
                time.sleep(.25)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('Embedding supervisor failed: ' + type(error).__name__, file=sys.stderr)
        sys.exit(1)
