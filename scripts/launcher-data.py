#!/usr/bin/env python3
"""Atomic, verified import into launcher-owned storage; run in an isolated helper.

Only /persist is writable. Legacy mounts are read-only and never deleted.
This helper never loads application settings or prints file contents.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import time
import uuid


def manifest(root):
    records = []
    if not root.is_dir() or root.is_symlink():
        raise RuntimeError('Legacy mount is not a real directory')
    for directory, dirs, files in os.walk(root, followlinks=False):
        for name in sorted(dirs + files):
            path = Path(directory) / name
            info = path.lstat()
            rel = path.relative_to(root).as_posix()
            mode = stat.S_IMODE(info.st_mode)
            if stat.S_ISLNK(info.st_mode):
                records.append([rel, 'link', os.readlink(path)])
            elif stat.S_ISDIR(info.st_mode):
                records.append([rel, 'dir', mode])
            elif stat.S_ISREG(info.st_mode):
                digest = hashlib.sha256()
                fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
                with os.fdopen(fd, 'rb') as source:
                    for chunk in iter(lambda: source.read(1 << 20), b''):
                        digest.update(chunk)
                    after = os.fstat(source.fileno())
                if (info.st_ino, info.st_size, info.st_mtime_ns) != (after.st_ino, after.st_size, after.st_mtime_ns):
                    raise RuntimeError('Legacy files changed during import; leave source intact and retry while idle')
                records.append([rel, 'file', mode, info.st_size, digest.hexdigest()])
            # Sockets/FIFOs/devices are transient execution objects, not portable data.
    return sorted(records)


def transient(directory, names):
    return [name for name in names if not (stat.S_ISREG((Path(directory) / name).lstat().st_mode)
            or stat.S_ISDIR((Path(directory) / name).lstat().st_mode)
            or stat.S_ISLNK((Path(directory) / name).lstat().st_mode))]


def set_owner(root, uid, gid):
    for directory, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            os.chown(Path(directory) / name, uid, gid, follow_symlinks=False)
        os.chown(directory, uid, gid, follow_symlinks=False)


def prepare_auth_directory(target, uid, gid, platform):
    # Compose creates all containers before health dependencies are satisfied.
    # Create only the empty directories so the CAD worker mounts only its
    # domain-separated key read-only; never give it sessions/settings or all of /data.
    for name in ('auth','cad-auth'):
        auth = Path(target) / 'data' / name
        if auth.is_symlink() or (auth.exists() and not auth.is_dir()):
            raise RuntimeError('Authentication directory is not a safe directory')
        if not auth.exists():
            auth.mkdir(mode=0o700)
            if platform == 'linux':
                os.chown(auth, uid, gid)



def prepare_locked(root, sources, uid=1000, gid=1000, platform='linux'):
    root = Path(root)
    if root.is_symlink():
        raise RuntimeError('Persistent root cannot be a symbolic link')
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    target = root / 'storage'
    if target.is_symlink():
        raise RuntimeError('Storage cannot be a symbolic link')
    if target.exists():
        receipt = target / 'migration.json'
        if receipt.is_symlink() or not receipt.is_file():
            raise RuntimeError('Existing storage has no import receipt; refuse to merge or overwrite it')
        data = json.loads(receipt.read_text())
        if data.get('format') != 1 or data.get('status') != 'complete':
            raise RuntimeError('Invalid storage receipt; source volumes remain untouched')
        for name in ('data', 'home'):
            if (target / name).is_symlink() or not (target / name).is_dir():
                raise RuntimeError('Storage directory is missing or symbolic; refuse empty reinitialization')
        if platform == 'linux' and (target.stat().st_uid, target.stat().st_gid) != (uid, gid):
            set_owner(target, uid, gid)
        prepare_auth_directory(target, uid, gid, platform)
        return {'status': 'existing', 'root': str(target)}
    stage = root / ('.storage-import-' + uuid.uuid4().hex)
    stage.mkdir(mode=0o700)
    proof = {'format': 1, 'status': 'complete', 'createdAt': int(time.time()), 'sources': {}}
    for name in ('data', 'home'):
        destination = stage / name
        source = sources.get(name)
        if source:
            source = Path(source)
            before = manifest(source)
            shutil.copytree(source, destination, symlinks=True, ignore=transient)
            if manifest(source) != before or manifest(destination) != before:
                raise RuntimeError('Import checksum/mode/link comparison failed; source and staging preserved')
        else:
            destination.mkdir(mode=0o700)
            before = []
        destination.chmod(0o700)
        digest = hashlib.sha256(json.dumps(before, ensure_ascii=True, separators=(',', ':')).encode()).hexdigest()
        proof['sources'][name] = {'imported': bool(source), 'entries': len(before), 'manifestSHA256': digest}
    receipt = stage / 'migration.json'
    with receipt.open('x') as output:
        json.dump(proof, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    receipt.chmod(0o600)
    if platform == 'linux':
        set_owner(stage, uid, gid)
    # A single rename commits both trees and their receipt together. Interrupted
    # imports remain separate staging folders; never resume by mixing old/new data.
    os.rename(stage, target)
    prepare_auth_directory(target, uid, gid, platform)
    return {'status': 'imported', 'root': str(target), 'sources': proof['sources']}


def prepare(root, sources, uid=1000, gid=1000, platform='linux'):
    root = Path(root)
    if root.is_symlink():
        raise RuntimeError('Persistent root cannot be a symbolic link')
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = os.open(root / '.prepare.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        return prepare_locked(root, sources, uid, gid, platform)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', default='/persist')
    parser.add_argument('--uid', type=int, default=1000)
    parser.add_argument('--gid', type=int, default=1000)
    parser.add_argument('--platform', default='linux')
    parser.add_argument('--data')
    parser.add_argument('--home')
    parser.add_argument('--seed-home', action='store_true')
    args = parser.parse_args()
    home_source = args.home or ('/home/aide' if args.seed_home else None)
    print(json.dumps(prepare(args.root, {'data': args.data, 'home': home_source}, args.uid, args.gid, args.platform)))
