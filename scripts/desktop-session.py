#!/usr/bin/env python3
"""Private stdio bootstrap for the Mac desktop host; never emits API keys."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import ssl
import subprocess
import sys


def run(root, args, timeout=30):
    result = subprocess.run(args, cwd=root, text=True, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Local service command failed; inspect the desktop startup log')
    return result.stdout.strip()


def sanitize(value):
    value = re.sub(r'(?i)(#token=|Bearer\s+|(?:api[_-]?key|password|access[_-]?token)\s*[:=]\s*)[^\s"\'<>]+',
                   r'\1[redacted]', value)
    return re.sub(r'\bsk-[A-Za-z0-9_.-]{8,}', '[redacted]', value)


def published_port(value):
    ports = []
    for line in value.splitlines():
        match = re.fullmatch(r'(?:[0-9.]+|\[[0-9a-fA-F:.%]+\]):([0-9]{1,5})', line.strip())
        if not match or not 1 <= int(match[1]) <= 65535:
            raise ValueError('Invalid local service port mapping')
        ports.append(int(match[1]))
    if not ports or len(set(ports)) != 1:
        raise ValueError('Missing or inconsistent local service port mapping')
    return ports[0]


def host_health(origin, certificate):
    # Match the canonical macOS launcher. Apple's older system Python/LibreSSL
    # can reject the same explicit local leaf anchor that system curl validates.
    # Ignore curlrc, proxies and redirects; keep hostname/expiry/TLS verification.
    result = subprocess.run(['curl', '--disable', '--silent', '--show-error', '--fail',
                             '--proto', '=https', '--noproxy', '*', '--max-time', '5',
                             '--max-filesize', '65536', '--cacert', str(certificate),
                             origin + '/healthz'], capture_output=True, timeout=8)
    if result.returncode or len(result.stdout) > 65536:
        raise ValueError('Invalid service identity response')
    return json.loads(result.stdout)


def session(root, start=False, command=run, health=host_health):
    root = Path(root).resolve(strict=True)
    if not (root / 'scripts/aide.sh').is_file() or not (root / 'compose.yaml').is_file():
        raise ValueError('Select an Aide installation directory')
    if start:
        for directory in (root / '.aide-data', root / '.aide-data/desktop'):
            if directory.is_symlink():
                raise ValueError('Desktop state must not be a symbolic link')
        env = dict(os.environ, AIDE_OPEN_BROWSER='0')
        result = subprocess.run(['bash', 'scripts/aide.sh', 'start'], cwd=root, env=env,
                                text=True, capture_output=True, timeout=240)
        logs = root / '.aide-data/desktop'
        logs.mkdir(parents=True, exist_ok=True, mode=0o700)
        log = logs / 'startup.log'
        if log.is_symlink():
            raise ValueError('Desktop startup log must not be a symbolic link')
        log.write_text(sanitize(result.stdout + result.stderr))
        log.chmod(0o600)
        if result.returncode:
            raise RuntimeError('Local service startup failed; check Docker and retry')
    docker = shutil.which('docker') or str(Path.home() / '.docker/bin/docker')
    def d(*args):
        return command(root, [docker, *args])
    cid = d('compose', 'ps', '--status', 'running', '-q', 'aide')
    if not re.fullmatch(r'[a-f0-9]{12,64}', cid):
        raise ValueError('Aide service is not running in this installation')
    info = json.loads(d('inspect', cid))[0]
    owner = info.get('Config', {}).get('Labels', {}).get('com.docker.compose.project.working_dir', '')
    if not owner or Path(owner).resolve() != root or not info.get('State', {}).get('Running'):
        raise ValueError('Local container belongs to another installation')
    port = published_port(d('compose', 'port', 'aide', '8080'))
    origin = 'https://localhost:' + str(port)
    certificate = root / '.aide-data/storage/data/certs/cert.pem'
    if certificate.is_symlink() or not certificate.is_file() or certificate.stat().st_size > 65536:
        raise ValueError('Local service certificate is unavailable')
    der = ssl.PEM_cert_to_DER_cert(certificate.read_text())
    inside = json.loads(d('exec', cid, 'curl', '-fsSk', '--max-time', '5', 'https://127.0.0.1:8080/healthz'))
    outside = health(origin, certificate)
    if (inside.get('service') != 'aide' or inside.get('status') != 'ok' or not inside.get('instanceId')
            or outside.get('service') != 'aide' or outside.get('status') != 'ok'
            or inside['instanceId'] != outside.get('instanceId')):
        raise ValueError('Local HTTPS service identity did not match the running container')
    token = d('exec', cid, 'sh', '-c', 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token')
    if not re.fullmatch(r'[A-Za-z0-9_.-]{16,512}', token):
        raise ValueError('Local access token is unavailable')
    return {'origin': origin, 'token': token, 'certificateSHA256': hashlib.sha256(der).hexdigest(),
            'instanceId': inside['instanceId']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True)
    parser.add_argument('--start', action='store_true')
    parser.add_argument('--private-stdio', action='store_true', required=True,
                        help='Return the local access token through a private app pipe')
    args = parser.parse_args()
    if sys.stdout.isatty():
        parser.error('Session credentials must be delivered through a private pipe')
    try:
        value = session(args.root, args.start)
        print(json.dumps(value))
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        # Neither upstream command output nor token-bearing URLs enter diagnostics.
        print(sanitize(str(error)), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
