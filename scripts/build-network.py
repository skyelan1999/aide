#!/usr/bin/env python3
"""Bounded public repository probes, usable on the host and inside Docker."""
import concurrent.futures
import json
import re
import urllib.parse
import sys
import time
import urllib.error
import urllib.request

ENDPOINTS = {
    'standard': {
        'registry': 'https://registry-1.docker.io/v2/library/python/manifests/3.12-slim-bookworm',
        'pip': 'https://pypi.org/simple/pypdf/',
        'apt': 'https://deb.debian.org/debian/dists/bookworm/InRelease',
    },
    'china': {
        'registry': 'https://docker.m.daocloud.io/v2/library/python/manifests/3.12-slim-bookworm',
        'pip': 'https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple/pypdf/',
        'apt': 'https://mirrors.tuna.tsinghua.edu.cn/debian/dists/bookworm/InRelease',
    },
}
PIP = {'standard': 'https://pypi.org/simple/', 'china': 'https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple/'}
PUBLIC_PIP = {
    PIP['standard'], PIP['china'], 'https://mirrors.aliyun.com/pypi/simple/',
    'https://pypi.tuna.tsinghua.edu.cn/simple/', 'https://mirrors.huaweicloud.com/repository/pypi/simple/',
}


def probe(kind, url):
    started = time.monotonic()
    try:
        request = urllib.request.Request(url, headers={'User-Agent': 'Aide-build-preflight/1',
            'Accept': 'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json'},
            method='HEAD' if kind == 'registry' else 'GET')
        with urllib.request.urlopen(request, timeout=4) as response:
            body = response.read(1024)
            if kind == 'apt' and b'BEGIN PGP SIGNED MESSAGE' not in body:
                raise ValueError('invalid Debian release response')
            if kind == 'pip' and b'<a ' not in body.lower():
                raise ValueError('invalid package index response')
            if kind == 'registry' and not re.fullmatch(r'sha256:[a-f0-9]{64}', response.headers.get('Docker-Content-Digest', '')):
                raise ValueError('invalid registry response')
        return {'ok': True, 'seconds': round(time.monotonic() - started, 2)}
    except urllib.error.HTTPError as error:
        if kind == 'registry' and error.code == 401:
            return authenticated_manifest(request, error.headers.get('WWW-Authenticate', ''), started)
        return {'ok': False, 'error': 'HTTP ' + str(error.code)}
    except (OSError, ValueError) as error:
        return {'ok': False, 'error': type(error).__name__}


def authenticated_manifest(request, challenge, started):
    fields = dict(re.findall(r'(realm|service|scope)="([^"]+)"', challenge))
    realm = urllib.parse.urlsplit(fields.get('realm', ''))
    registry = urllib.parse.urlsplit(request.full_url).hostname
    if (not challenge.lower().startswith('bearer ') or realm.scheme != 'https' or
            realm.hostname not in ('auth.docker.io', 'm.daocloud.io', registry) or realm.username or realm.password):
        return {'ok': False, 'error': 'unsupported registry authentication'}
    try:
        url = fields['realm'] + '?' + urllib.parse.urlencode({k: v for k, v in fields.items() if k != 'realm'})
        with urllib.request.urlopen(url, timeout=4) as response:
            token = json.loads(response.read(65536)).get('token')
        if not isinstance(token, str) or not token:
            raise ValueError('missing registry token')
        request.add_header('Authorization', 'Bearer ' + token)
        with urllib.request.urlopen(request, timeout=4) as response:
            digest = response.headers.get('Docker-Content-Digest', '')
            if not re.fullmatch(r'sha256:[a-f0-9]{64}', digest):
                raise ValueError('missing manifest digest')
        return {'ok': True, 'seconds': round(time.monotonic() - started, 2)}
    except (OSError, ValueError) as error:
        return {'ok': False, 'error': type(error).__name__}


def assess():
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        jobs = {(region, kind): pool.submit(probe, kind, url)
                for region, entries in ENDPOINTS.items() for kind, url in entries.items()}
        return {region: {kind: jobs[region, kind].result() for kind in ENDPOINTS[region]}
                for region in ENDPOINTS}


def select(host, container, mode='auto', fallback=True):
    if mode not in ('auto', 'standard', 'china'):
        raise ValueError('AIDE_BUILD_SOURCE must be auto, standard or china')
    if mode != 'auto':
        return {kind: mode for kind in ('registry', 'pip', 'apt')}
    result = {}
    for kind in ('registry', 'pip', 'apt'):
        # Registry paths are consumed by the engine/BuildKit. Host probes are a
        # hint only. The exact image pull and digest verification still follow.
        network = host if kind == 'registry' or container is None else container
        result[kind] = 'standard' if network['standard'][kind]['ok'] or not fallback else (
            'china' if network['china'][kind]['ok'] else 'unavailable')
    return result


if __name__ == '__main__':
    print(json.dumps(assess()))
