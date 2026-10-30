#!/usr/bin/env python3
"""Aide file reference relay. Python 3.10+; install: python -m pip install cryptography
Run this downloaded file manually on the device containing the shared directory.
The script contains an access secret: keep it outside that directory and private.
Only CONFIG['root'] is shared. Writes require both the Aide lock and CONFIG['rw'].
No shell, account login, background installation or arbitrary target forwarding.
"""
import base64
import argparse
import contextlib
import ctypes
import datetime
import errno
import hashlib
import hmac
import json
import os
from pathlib import Path
import socket
import ssl
import stat
import sys
import tempfile
import threading
import uuid
import time
import shutil
import re

CONFIG = json.loads(__CONFIG_JSON__)
MAX_FILE = 64 * 1024 * 1024
MAX_LIST = 2000
MAX_LIST_BYTES = 1024 * 1024
MODES = {'probe', 'list', 'read', 'write', 'mkdir', 'rename', 'remove', 'stat', 'space', 'openread', 'openwrite', 'readat', 'writeat', 'hstat', 'truncate', 'sync', 'close'}
WRITES = {'write', 'mkdir', 'rename', 'remove', 'openwrite', 'writeat', 'truncate', 'sync'}


def parts(value, root_ok=False):
    if not isinstance(value, str) or len(value.encode('utf-8')) > 4096:
        raise ValueError('invalid path')
    if root_ok and value in ('', '.'):
        return []
    result = value.split('/')
    if any(c in value for c in '\\\x00\r\n:') or any(x in ('', '.', '..') or x.startswith('.aide-relay-') for x in result):
        raise ValueError('invalid relative path')
    return result


def exact(sock, count):
    data = bytearray()
    while len(data) < count:
        piece = sock.recv(min(65536, count - len(data)))
        if not piece:
            raise ConnectionError('closed')
        data.extend(piece)
    return bytes(data)


def line(sock):
    data = bytearray()
    while len(data) < 4096:
        char = exact(sock, 1)
        if char == b'\n':
            value = json.loads(data)
            if not isinstance(value, dict):
                raise ValueError('invalid header')
            return value
        data.extend(char)
    raise ValueError('header too large')


def send(sock, value):
    sock.sendall(json.dumps(value, separators=(',', ':')).encode() + b'\n')


def configuration():
    text = 'aide-file-relay-config-v1\x00' + CONFIG['id'] + '\x00' + CONFIG['root']
    return hashlib.sha256(text.encode()).hexdigest()


def identity(info):
    return info.st_dev, info.st_ino


if os.name == 'nt':
    import ctypes
    from ctypes import wintypes
    import msvcrt
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.CreateFileW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD, ctypes.c_void_p, wintypes.DWORD, wintypes.DWORD, wintypes.HANDLE]
    kernel.CreateFileW.restype = wintypes.HANDLE
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    class WinInfo(ctypes.Structure):
        _fields_ = [('attributes', wintypes.DWORD), ('creation', wintypes.FILETIME), ('access', wintypes.FILETIME), ('write', wintypes.FILETIME), ('volume', wintypes.DWORD), ('sizeHigh', wintypes.DWORD), ('sizeLow', wintypes.DWORD), ('links', wintypes.DWORD), ('indexHigh', wintypes.DWORD), ('indexLow', wintypes.DWORD)]
    kernel.GetFileInformationByHandle.argtypes = [wintypes.HANDLE, ctypes.POINTER(WinInfo)]
    kernel.GetFileInformationByHandle.restype = wintypes.BOOL

    def win_regular_handle(fd):
        info = WinInfo()
        if not kernel.GetFileInformationByHandle(msvcrt.get_osfhandle(fd), ctypes.byref(info)) or info.attributes & 0x400:
            raise PermissionError('reparse point or unreadable handle')

    kernel.GetFinalPathNameByHandleW.argtypes = [wintypes.HANDLE, wintypes.LPWSTR, wintypes.DWORD, wintypes.DWORD]
    kernel.GetFinalPathNameByHandleW.restype = wintypes.DWORD

    def win_final(handle):
        buffer = ctypes.create_unicode_buffer(32768)
        size = kernel.GetFinalPathNameByHandleW(handle, buffer, len(buffer), 0)
        if not size or size >= len(buffer):
            raise OSError('cannot verify final path')
        return os.path.normcase(buffer.value)

    def win_open(path, directory):
        # No FILE_SHARE_DELETE: keep every opened ancestor stable during the RPC.
        handle = kernel.CreateFileW(str(path), 0x80000000, 3, None, 3, 0x02200000, None)
        if handle == ctypes.c_void_p(-1).value:
            code = ctypes.get_last_error()
            if code in (2, 3):
                raise FileNotFoundError('not found')
            raise PermissionError('cannot open path')
        try:
            fd = msvcrt.open_osfhandle(handle, os.O_RDONLY | os.O_BINARY)
        except Exception:
            kernel.CloseHandle(handle)
            raise
        info = os.fstat(fd)
        if getattr(info, 'st_file_attributes', 0) & 0x400 or (directory and not stat.S_ISDIR(info.st_mode)):
            os.close(fd)
            raise PermissionError('reparse point or invalid directory')
        return fd


def rename_exclusive(source, destination, source_parent, target_parent):
    if os.name == 'nt':
        os.rename(source, destination)  # Windows never replaces an existing target.
        return
    library = ctypes.CDLL(None, use_errno=True)
    if sys.platform == 'darwin':
        rename = library.renameatx_np
        flag = 4  # RENAME_EXCL from the macOS SDK stdio.h.
    elif sys.platform.startswith('linux'):
        rename = library.renameat2
        flag = 1  # RENAME_NOREPLACE.
    else:
        raise PermissionError('atomic no-replace rename unavailable')
    rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    rename.restype = ctypes.c_int
    if rename(source_parent, os.fsencode(source), target_parent, os.fsencode(destination), flag):
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))


class SharedRoot:
    def __init__(self, root, private):
        self.path = Path(root).resolve(strict=True)
        if not self.path.is_dir():
            raise ValueError('shared root must be a directory')
        self.private = {identity(Path(p).stat()) for p in private}
        if os.name == 'nt':
            self.fd = win_open(self.path, True)
        else:
            self.fd = os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.check(self.fd)

    def close(self):
        os.close(self.fd)

    def check(self, fd):
        info = os.fstat(fd)
        if identity(info) in self.private:
            raise PermissionError('private relay material')
        if os.name == 'nt':
            win_regular_handle(fd)
            root = win_final(msvcrt.get_osfhandle(self.fd))
            actual = win_final(msvcrt.get_osfhandle(fd))
            if os.path.commonpath([root, actual]) != root:
                raise PermissionError('outside shared root')
        return info

    @contextlib.contextmanager
    def directory(self, components, create=False):
        held = [os.dup(self.fd)] if os.name != 'nt' else []
        current = self.fd if os.name == 'nt' else held[0]
        try:
            for component in components:
                if os.name == 'nt':
                    name = win_final(msvcrt.get_osfhandle(current)) + '\\' + component
                    if create:
                        try:
                            os.mkdir(name)
                        except FileExistsError:
                            pass
                    following = win_open(name, True)
                else:
                    if create:
                        try:
                            os.mkdir(component, 0o755, dir_fd=current)
                        except FileExistsError:
                            pass
                    following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=current)
                held.append(following)
                current = following
                self.check(current)
            yield current
        finally:
            for fd in reversed(held):
                os.close(fd)

    def filename(self, parent, name):
        return win_final(msvcrt.get_osfhandle(parent)) + '\\' + name if os.name == 'nt' else name

    def kwargs(self, parent):
        return {} if os.name == 'nt' else {'dir_fd': parent}

    def inspect(self, parent, name):
        info = os.stat(self.filename(parent, name), follow_symlinks=False, **self.kwargs(parent))
        if stat.S_ISLNK(info.st_mode) or getattr(info, 'st_file_attributes', 0) & 0x400 or identity(info) in self.private:
            raise PermissionError('link or private relay material')
        if not stat.S_ISREG(info.st_mode) and not stat.S_ISDIR(info.st_mode):
            raise PermissionError('special file')
        return info

    def metadata(self, value):
        components = parts(value, True)
        if not components:
            info = self.check(self.fd)
        else:
            with self.directory(components[:-1]) as parent:
                info = self.inspect(parent, components[-1])
        return file_info(info, components[-1] if components else '.')

    def open_file(self, value, request, write=False):
        components = parts(value)
        with self.directory(components[:-1]) as parent:
            try:
                info = self.inspect(parent, components[-1])
                if not stat.S_ISREG(info.st_mode):
                    raise PermissionError('not a file')
                if write and request.get('exclusive'):
                    raise FileExistsError('destination exists')
            except FileNotFoundError:
                if not write or not request.get('create'):
                    raise
            name = self.filename(parent, components[-1])
            flags = (os.O_RDWR if write else os.O_RDONLY) | getattr(os, 'O_BINARY', 0) | getattr(os, 'O_NONBLOCK', 0)
            if write:
                if request.get('create'): flags |= os.O_CREAT
                if request.get('exclusive'): flags |= os.O_EXCL
            if os.name == 'nt':
                # CreateFile's anchored-parent sharing prevents replacement.
                access = 0xC0000000 if write else 0x80000000
                disposition = 1 if flags & os.O_EXCL else 4 if flags & os.O_CREAT else 3
                handle = kernel.CreateFileW(name, access, 3, None, disposition, 0x00200000, None)
                if handle == ctypes.c_void_p(-1).value:
                    code = ctypes.get_last_error()
                    if code in (2, 3): raise FileNotFoundError('not found')
                    if code in (80, 183): raise FileExistsError('exists')
                    raise PermissionError('cannot open file')
                fd = msvcrt.open_osfhandle(handle, flags & (os.O_RDWR | os.O_BINARY))
            else:
                fd = os.open(name, flags | os.O_NOFOLLOW, 0o600, dir_fd=parent)
            try:
                info = self.check(fd)
                if not stat.S_ISREG(info.st_mode): raise PermissionError('not regular')
                if write and request.get('truncate'): os.ftruncate(fd, 0)
                return fd
            except Exception:
                os.close(fd)
                raise

    def listing(self, value):
        components = parts(value, True)
        result = []
        with self.directory(components) as parent:
            target = self.filename(parent, '') if os.name == 'nt' else parent
            with os.scandir(target) as entries:
                for entry in entries:
                    try:
                        parts(entry.name)
                        info = self.inspect(parent, entry.name)
                    except (OSError, ValueError):
                        continue
                    if len(result) >= MAX_LIST:
                        raise ValueError('directory exceeds 2000 entries')
                    result.append({'name': entry.name, 'path': '/'.join(components + [entry.name]),
                                   'dir': stat.S_ISDIR(info.st_mode), 'size': info.st_size, 'sizeBytes': str(info.st_size), 'modifiedNano': str(info.st_mtime_ns),
                                   'modified': datetime.datetime.fromtimestamp(info.st_mtime, datetime.timezone.utc).isoformat()})
        data = json.dumps(result, ensure_ascii=True, separators=(',', ':')).encode()
        if len(data) > MAX_LIST_BYTES:
            raise ValueError('directory response too large')
        return data

    def read(self, value, limit, sock):
        components = parts(value)
        with self.directory(components[:-1]) as parent:
            self.inspect(parent, components[-1])
            if os.name == 'nt':
                fd = win_open(self.filename(parent, components[-1]), False)
            else:
                fd = os.open(components[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
            with os.fdopen(fd, 'rb') as source:
                info = self.check(source.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_size > limit:
                    raise ValueError('file exceeds read limit or is not regular')
                send(sock, {'ok': True, 'size': info.st_size})
                digest = hashlib.sha256()
                remaining = info.st_size
                while remaining:
                    data = source.read(min(65536, remaining))
                    if not data:
                        raise ConnectionError('file changed during read')
                    sock.sendall(data)
                    digest.update(data)
                    remaining -= len(data)
                send(sock, {'ok': True, 'sha256': digest.hexdigest()})

    def write(self, value, size, exclusive, sock, expected):
        components = parts(value)
        with self.directory(components[:-1], create=True) as parent:
            name = components[-1]
            try:
                info = self.inspect(parent, name)
                if exclusive:
                    raise FileExistsError('destination exists')
                if not stat.S_ISREG(info.st_mode):
                    raise PermissionError('destination is not a regular file')
            except FileNotFoundError:
                pass
            temporary = '.aide-relay-' + uuid.uuid4().hex
            kwargs = self.kwargs(parent)
            temp = self.filename(parent, temporary)
            target = self.filename(parent, name)
            fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, 'O_BINARY', 0), 0o600, **kwargs)
            try:
                send(sock, {'ok': True, 'ready': True})
                with os.fdopen(fd, 'wb') as output:
                    digest = hashlib.sha256()
                    remaining = size
                    while remaining:
                        data = exact(sock, min(65536, remaining))
                        output.write(data)
                        digest.update(data)
                        remaining -= len(data)
                    if not hmac.compare_digest(digest.hexdigest(), expected):
                        raise ValueError('upload checksum mismatch')
                    output.flush()
                    os.fsync(output.fileno())
                # Hard-link publication supplies atomic no-replace semantics.
                both = {} if os.name == 'nt' else {'src_dir_fd': parent, 'dst_dir_fd': parent}
                if exclusive:
                    os.link(temp, target, follow_symlinks=False, **both)
                    os.unlink(temp, **kwargs)
                else:
                    try:
                        self.inspect(parent, name)
                    except FileNotFoundError:
                        pass
                    os.replace(temp, target, **both)
                send(sock, {'ok': True})
            finally:
                try:
                    os.unlink(temp, **kwargs)
                except FileNotFoundError:
                    pass

    def mutate(self, mode, request):
        components = parts(request.get('path'))
        with self.directory(components[:-1]) as parent:
            name = self.filename(parent, components[-1])
            if mode == 'mkdir':
                os.mkdir(name, 0o755, **self.kwargs(parent))
            elif mode == 'remove':
                info = self.inspect(parent, components[-1])
                (os.rmdir if stat.S_ISDIR(info.st_mode) else os.unlink)(name, **self.kwargs(parent))
            elif mode == 'rename':
                self.inspect(parent, components[-1])
                destination = parts(request.get('destination'))
                with self.directory(destination[:-1]) as target_parent:
                    try:
                        self.inspect(target_parent, destination[-1])
                    except FileNotFoundError:
                        pass
                    else:
                        raise FileExistsError('destination exists')
                    # Parent anchors prevent traversal/races; the server lock
                    # serializes publication by this agent. Never follow links.
                    both = {} if os.name == 'nt' else {'src_dir_fd': parent, 'dst_dir_fd': target_parent}
                    rename_exclusive(name, self.filename(target_parent, destination[-1]), parent, target_parent)



def file_info(info, name):
    return {'name': name, 'size': info.st_size, 'dir': stat.S_ISDIR(info.st_mode),
            'permissions': stat.S_IMODE(info.st_mode), 'modifiedNano': info.st_mtime_ns}


class FileHandles:
    def __init__(self):
        self.items = {}
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.reaper = threading.Thread(target=self.expire, daemon=True)
        self.reaper.start()

    def add(self, root, request, writable):
        with self.lock:
            if len(self.items) >= 128: raise PermissionError('too many open files')
            fd = root.open_file(request.get('path'), request, writable)
            token = uuid.uuid4().hex
            self.items[token] = {'fd': fd, 'writable': writable, 'name': parts(request['path'])[-1], 'used': time.monotonic(), 'lock': threading.Lock()}
            return token, file_info(os.fstat(fd), self.items[token]['name'])

    @contextlib.contextmanager
    def get(self, token, writable=False):
        with self.lock:
            item = self.items.get(token)
            if not item: raise FileNotFoundError('file handle expired')
            # Acquire under the table lock so expiration/close cannot reuse its FD.
            if not item['lock'].acquire(timeout=10): raise PermissionError('file busy')
            item['used'] = time.monotonic()
        try:
            if writable and not item['writable']: raise PermissionError('read-only handle')
            yield item
        finally:
            item['lock'].release()

    def close(self, token):
        with self.lock:
            item = self.items.get(token)
            if item:
                if not item['lock'].acquire(timeout=10): raise PermissionError('file busy')
                self.items.pop(token)
        if item:
            try: os.close(item['fd'])
            finally: item['lock'].release()

    def expire(self):
        while not self.stop.wait(10):
            with self.lock:
                for token, item in list(self.items.items()):
                    if time.monotonic() - item['used'] > 120 and item['lock'].acquire(blocking=False):
                        self.items.pop(token)
                        try: os.close(item['fd'])
                        finally: item['lock'].release()

    def close_all(self):
        self.stop.set()
        for token in list(self.items): self.close(token)


def handle_operation(mode, request, sock, root, handles):
    if mode in ('openread', 'openwrite'):
        token, info = handles.add(root, request, mode == 'openwrite')
        send(sock, {'ok': True, 'handle': token, 'info': info})
        return
    if mode == 'stat':
        send(sock, {'ok': True, 'info': root.metadata(request.get('path', '.'))})
        return
    if mode == 'space':
        if os.name == 'nt':
            available = shutil.disk_usage(win_final(msvcrt.get_osfhandle(root.fd))).free
        else:
            info = os.fstatvfs(root.fd)
            available = info.f_bavail * info.f_frsize
        send(sock, {'ok': True, 'available': available})
        return
    token = request.get('handle')
    if not isinstance(token, str) or not re.fullmatch(r'[a-f0-9]{32}', token): raise ValueError('invalid handle')
    if mode == 'close':
        handles.close(token)
        send(sock, {'ok': True})
        return
    with handles.get(token, mode in WRITES) as item:
        fd = item['fd']
        if mode in ('readat', 'writeat'):
            offset, size = request.get('offset'), request.get('size')
            if type(offset) is not int or type(size) is not int or not 0 <= offset <= (1 << 63) - 1 or not 0 <= size <= 4 * 1024 * 1024 or offset + size >= 1 << 63:
                raise ValueError('invalid byte range')
            if mode == 'readat':
                os.lseek(fd, offset, os.SEEK_SET)
                data = bytearray()
                while len(data) < size:
                    value = os.read(fd, size - len(data))
                    if not value: break
                    data.extend(value)
                send(sock, {'ok': True, 'size': len(data)})
                sock.sendall(data)
                send(sock, {'ok': True, 'sha256': hashlib.sha256(data).hexdigest()})
            else:
                expected = request.get('sha256')
                if not isinstance(expected, str) or not re.fullmatch(r'[a-f0-9]{64}', expected): raise ValueError('invalid digest')
                send(sock, {'ok': True, 'ready': True})
                data = exact(sock, size)
                if not hmac.compare_digest(hashlib.sha256(data).hexdigest(), expected): raise ValueError('chunk checksum mismatch')
                os.lseek(fd, offset, os.SEEK_SET)
                position = 0
                while position < len(data):
                    count = os.write(fd, data[position:])
                    if not count: raise OSError('short write')
                    position += count
                os.fsync(fd)
                send(sock, {'ok': True})
        elif mode == 'hstat': send(sock, {'ok': True, 'info': file_info(os.fstat(fd), item['name'])})
        elif mode == 'truncate':
            size = request.get('size')
            if type(size) is not int or not 0 <= size < 1 << 63: raise ValueError('invalid size')
            os.ftruncate(fd, size)
            send(sock, {'ok': True})
        elif mode == 'sync':
            os.fsync(fd)
            send(sock, {'ok': True})
        else: raise ValueError('unknown operation')


def make_context(directory, key):
    try:
        from cryptography import x509
        from cryptography.hazmat.primitives import serialization
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.x509.oid import NameOID
    except ImportError:
        raise RuntimeError('Install dependency: python -m pip install cryptography')
    seed = hmac.new(key, b'aide-file-relay-certificate-v1', hashlib.sha256).digest()
    private = Ed25519PrivateKey.from_private_bytes(seed)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'Aide file reference relay')])
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(private.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=365)).sign(private, None)
    for filename, data in [('certificate.pem', certificate.public_bytes(serialization.Encoding.PEM)), ('identity.pem', private.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))]:
        fd = os.open(os.path.join(directory, filename), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as output:
            output.write(data)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(os.path.join(directory, 'certificate.pem'), os.path.join(directory, 'identity.pem'))
    return context


slots = threading.BoundedSemaphore(8)
mutation = threading.Lock()


def client(raw, context, key, root, handles):
    sock = None
    authenticated = False
    timer = None
    try:
        raw.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        raw.settimeout(30)
        sock = context.wrap_socket(raw, server_side=True)
        def expire():
            try: sock.shutdown(socket.SHUT_RDWR)
            except OSError: pass
        timer = threading.Timer(60, expire)
        timer.daemon = True
        timer.start()
        nonce = base64.b64encode(os.urandom(32)).decode()
        send(sock, {'version': 1, 'challenge': nonce})
        auth = line(sock)
        mode = auth.get('mode')
        digest = configuration()
        message = 'aide-file-relay-v1:' + nonce + ':' + str(mode) + ':' + CONFIG['id'] + ':' + digest
        proof = hmac.new(key, message.encode(), hashlib.sha256).hexdigest()
        if mode not in MODES or auth.get('id') != CONFIG['id'] or auth.get('configuration') != digest or not hmac.compare_digest(str(auth.get('proof', '')), proof):
            raise PermissionError('authentication or deployed configuration mismatch')
        authenticated = True
        send(sock, {'ok': True, 'id': CONFIG['id'], 'kind': 'files', 'configuration': digest, 'writable': bool(CONFIG['rw'])})
        if mode == 'probe':
            return
        request = line(sock)
        if mode in WRITES:
            if not CONFIG['rw']:
                raise PermissionError('deployed relay is read-only; download and redeploy a writable script')
            if not mutation.acquire(timeout=10):
                raise PermissionError('relay busy')
            try:
                if mode in ('openwrite','writeat','truncate','sync'):
                    handle_operation(mode, request, sock, root, handles)
                elif mode == 'write':
                    size = request.get('size')
                    if type(size) is not int or not 0 <= size <= MAX_FILE or not re.fullmatch(r'[a-f0-9]{64}', str(request.get('sha256',''))):
                        raise ValueError('upload exceeds limit')
                    root.write(request.get('path'), size, request.get('exclusive') is True, sock, request['sha256'])
                else:
                    root.mutate(mode, request)
                    send(sock, {'ok': True})
            finally:
                mutation.release()
        elif mode == 'list':
            data = root.listing(request.get('path', '.'))
            send(sock, {'ok': True, 'size': len(data)})
            sock.sendall(data)
            send(sock, {'ok': True, 'sha256': hashlib.sha256(data).hexdigest()})
        elif mode == 'read':
            limit = request.get('limit')
            if type(limit) is not int or not 0 < limit <= MAX_FILE:
                raise ValueError('invalid read limit')
            root.read(request.get('path'), limit, sock)
        else:
            handle_operation(mode, request, sock, root, handles)
    except Exception as error:
        if sock:
            code = 'failed'
            if isinstance(error, FileNotFoundError):
                code = 'not-found'
            elif isinstance(error, FileExistsError):
                code = 'exists'
            elif isinstance(error, PermissionError):
                code = 'denied'
            # Never expose absolute OS paths, credentials or arbitrary exception text.
            message = 'relay authentication failed' if not authenticated else {
                'not-found': 'file or directory not found', 'exists': 'destination exists',
                'denied': 'operation denied; check source/script write permissions and path',
                'failed': 'relay operation failed; check path, file size and deployed script',
            }[code]
            try:
                send(sock, {'ok': False, 'code': code, 'error': message})
            except Exception:
                pass
    finally:
        if timer: timer.cancel()
        if sock:
            sock.close()
        raw.close()
        slots.release()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--listen-host', default=CONFIG.get('listenHost', '0.0.0.0'), help='Local bind address')
    args = parser.parse_args()
    if not Path(CONFIG['root']).is_absolute():
        raise ValueError('Configure an absolute remote shared directory and download again')
    key = base64.b64decode(CONFIG['key'], validate=True)
    if len(key) != 32:
        raise ValueError('Invalid relay key')
    with tempfile.TemporaryDirectory(prefix='aide-file-relay-') as directory:
        context = make_context(directory, key)
        root = SharedRoot(CONFIG['root'], [__file__, directory])
        handles = FileHandles()
        listener = socket.socket(socket.AF_INET6 if ':' in args.listen_host else socket.AF_INET)
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind((args.listen_host, CONFIG['listenPort']))
        listener.listen(16)
        print('Aide file relay ready on TCP port ' + str(listener.getsockname()[1]), flush=True)
        try:
            while True:
                raw, _ = listener.accept()
                if not slots.acquire(blocking=False):
                    raw.close()
                    continue
                threading.Thread(target=client, args=(raw, context, key, root, handles), daemon=True).start()
        finally:
            listener.close()
            handles.close_all()
            root.close()


if __name__ == '__main__':
    try:
        main()
    except KeyboardInterrupt:
        pass
    except Exception:
        sys.exit('File relay could not start. Check Python dependencies, shared directory and listening port.')
