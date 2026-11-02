#!/usr/bin/env python3
"""Build pinned public host components; runtime never downloads source.

Prepare on the host with --prepare-only to avoid container network dependency.
Every local, cached or downloaded archive is checked against the source lock.
"""
import argparse
import hashlib
import http.client
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request

MAX_SOURCE = 32 << 20


def source_lock(pin):
    lock = json.loads(pathlib.Path(pin).read_text())
    if not isinstance(lock, dict):
        raise ValueError('Invalid source lock')
    for key, pattern in (("commit", r"[a-f0-9]{40}"), ("sha256", r"[a-f0-9]{64}"), ("version", r"\d+\.\d+\.\d+")):
        if not isinstance(lock.get(key), str) or not re.fullmatch(pattern, lock[key]):
            raise ValueError("Invalid source lock")
    return lock


def checked_archive(path, digest):
    path = pathlib.Path(path)
    if path.is_symlink():
        raise ValueError("Source archive must not be a symbolic link")
    size, checksum = 0, hashlib.sha256()
    with path.open('rb') as source:
        while True:
            chunk = source.read(1 << 20)
            if not chunk:
                break
            size += len(chunk)
            if size > MAX_SOURCE:
                raise ValueError("Source archive too large")
            checksum.update(chunk)
    if checksum.hexdigest() != digest:
        raise ValueError("Source checksum mismatch")
    return path


def cache_directory(path):
    path = pathlib.Path(path)
    if path.is_symlink():
        raise ValueError("Source cache directory must not be a symbolic link")
    path.mkdir(parents=True, exist_ok=True)
    return path


def verified_copy(source, destination, digest):
    destination = pathlib.Path(destination)
    directory = cache_directory(destination.parent)
    if destination.is_symlink():
        raise ValueError("Source archive destination must not be a symbolic link")
    with tempfile.NamedTemporaryFile(dir=directory, prefix='.source-', suffix='.tar.gz', delete=False) as out:
        temporary = pathlib.Path(out.name)
    try:
        with pathlib.Path(source).open('rb') as src, temporary.open('wb') as dst:
            size = 0
            while True:
                chunk = src.read(1 << 20)
                if not chunk:
                    break
                size += len(chunk)
                if size > MAX_SOURCE:
                    raise ValueError('Source archive too large')
                dst.write(chunk)
        checked_archive(temporary, digest)
        os.replace(temporary, destination)
    finally:
        temporary.unlink(missing_ok=True)
    return destination


def download_source(lock, destination):
    url = 'https://codeload.github.com/skyelan1999/aide-pkg-CodexBridge/tar.gz/' + lock['commit']
    for attempt in range(3):
        with tempfile.NamedTemporaryFile(dir=destination.parent, prefix='.source-', suffix='.tar.gz', delete=False) as out:
            temporary = pathlib.Path(out.name)
        try:
            with urllib.request.urlopen(url, timeout=20) as response, temporary.open('wb') as out:
                size = 0
                while True:
                    chunk = response.read(1 << 20)
                    if not chunk:
                        break
                    size += len(chunk)
                    if size > MAX_SOURCE:
                        raise ValueError('Source archive too large')
                    out.write(chunk)
            checked_archive(temporary, lock['sha256'])
            os.replace(temporary, destination)
            return destination
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead) as failure:
            if isinstance(failure, urllib.error.HTTPError) and failure.code not in (408, 429, 500, 502, 503, 504):
                raise RuntimeError('Codex Bridge source download rejected (HTTP %d)' % failure.code) from None
            if attempt == 2:
                raise RuntimeError('Cannot download pinned Codex Bridge source after 3 attempts; check host network or provide --archive. Existing service was not changed.') from None
            print('Codex Bridge source: temporary connection failure; retry %d/3' % (attempt + 2), file=sys.stderr, flush=True)
            time.sleep(0.5 * (attempt + 1))
        finally:
            temporary.unlink(missing_ok=True)


def prepare_source(lock, archive=None, cache_dir=None, bundle_dir=None):
    digest = lock['sha256']
    if archive:
        return checked_archive(archive, digest)
    if bundle_dir:
        bundled = pathlib.Path(bundle_dir) / (digest + '.tar.gz')
        if bundled.exists() or bundled.is_symlink():
            return checked_archive(bundled, digest)
    cache = cache_directory(cache_dir or pathlib.Path.home() / '.cache/aide/codex-host-sources')
    source = cache / (digest + '.tar.gz')
    if source.exists() or source.is_symlink():
        # Never follow links, and never trust a previous download without hashing.
        if source.is_symlink():
            raise ValueError('Source cache entry must not be a symbolic link')
        try:
            return checked_archive(source, digest)
        except ValueError:
            print('Codex Bridge source cache failed checksum/size validation; downloading a verified replacement.', file=sys.stderr, flush=True)
    return download_source(lock, source)


def build(pin, out, archive=None, cache_dir=None, bundle_dir='docker/host-sources'):
    lock = source_lock(pin)
    version, digest = lock['version'], lock['sha256']
    archive = prepare_source(lock, archive, cache_dir, bundle_dir)
    with tempfile.TemporaryDirectory(prefix='codex-host-build-') as tmp:
        tmp = pathlib.Path(tmp)
        # Extract only our private, verified copy, not a mutable shared cache file.
        source = verified_copy(archive, tmp / 'source.tar.gz', digest)
        root=tmp/'source';root.mkdir();size=0
        with tarfile.open(source) as tar:
            for member in tar:
                relative=pathlib.PurePosixPath(member.name)
                if relative.is_absolute() or '..' in relative.parts or not (member.isfile() or member.isdir()):raise ValueError('Unsafe source archive')
                if len(relative.parts)<2:continue
                dest=root.joinpath(*relative.parts[1:]);size+=member.size
                if size>128<<20:raise ValueError('Expanded source too large')
                if member.isdir():dest.mkdir(parents=True,exist_ok=True)
                else:
                    dest.parent.mkdir(parents=True,exist_ok=True)
                    with tar.extractfile(member) as src,dest.open('wb') as dst:shutil.copyfileobj(src,dst)
        manifest=json.loads((root/'manifest.json').read_text())
        if manifest['version']!=version:raise ValueError('Version mismatch')
        subprocess.run(['node','scripts/build.cjs'],cwd=root,check=True)
        subprocess.run(['python3','scripts/package-codex-bridge.py','--out','dist'],cwd=root,check=True)
        shutil.copyfile(root/'dist'/('codex-bridge-'+version+'.zip'),root/'cmd/host-manager/payload.zip')
        out=pathlib.Path(out);out.mkdir(parents=True,exist_ok=True);sums=[]
        for platform in ('darwin','linux','windows'):
            for arch in ('amd64','arm64'):
                name='codex-bridge-'+platform+'-'+arch+('.exe' if platform=='windows' else '')
                destination=(out/name).resolve()
                flags='-s -w'+(' -H=windowsgui' if platform=='windows' else '')
                subprocess.run(['go','build','-trimpath','-ldflags',flags,'-o',str(destination),'.'],cwd=root/'cmd/host-manager',env={**os.environ,'CGO_ENABLED':'0','GOOS':platform,'GOARCH':arch,'GOWORK':'off'},check=True)
                sums.append(hashlib.sha256(destination.read_bytes()).hexdigest()+'  '+name)
        (out/'SHA256SUMS').write_text('\n'.join(sums)+'\n');(out/'VERSION').write_text(version+'\n');shutil.copyfile(root/'LICENSE',out/'LICENSE-CodexBridge.txt')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pin', default='docker/codex-host-source.json')
    parser.add_argument('--out', default='/opt/aide-host-components')
    parser.add_argument('--archive')
    parser.add_argument('--cache-dir')
    parser.add_argument('--bundle-dir', default='docker/host-sources')
    parser.add_argument('--prepare-only', action='store_true', help='Verify/download source and stage it for Docker; do not compile')
    args = parser.parse_args()
    try:
        if args.prepare_only:
            lock = source_lock(args.pin)
            source = prepare_source(lock, args.archive, args.cache_dir, args.bundle_dir)
            destination = pathlib.Path(args.bundle_dir) / (lock['sha256'] + '.tar.gz')
            if source != destination:
                verified_copy(source, destination, lock['sha256'])
            print('Codex Bridge source ready: ' + lock['commit'] + ' (SHA-256 verified)', flush=True)
        else:
            build(args.pin, args.out, args.archive, args.cache_dir, args.bundle_dir)
    except (OSError, RuntimeError, ValueError, tarfile.TarError, subprocess.CalledProcessError) as failure:
        print('Codex Bridge build: ' + str(failure), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
