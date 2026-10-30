#!/usr/bin/env python3
"""Cross-platform filesystem boundaries for the exact embedded/downloaded relay."""
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import types
import unittest

ROOT = Path(__file__).resolve().parents[1]
agent = types.ModuleType('reference_relay_test')
source = (ROOT / 'internal/server/relay/agent.py').read_text(encoding='utf-8')
exec(compile(source.replace('__CONFIG_JSON__', repr(json.dumps({'id': 'test', 'root': '/unused'}))), 'agent.py', 'exec'), agent.__dict__)


class Socket:
    def __init__(self, data=b''):
        self.input = io.BytesIO(data)
        self.output = bytearray()

    def recv(self, size):
        return self.input.read(size)

    def sendall(self, data):
        self.output.extend(data)


class RelayBoundaries(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        self.secret = self.path / 'agent.py'
        self.secret.write_bytes(b'key inside downloaded script')
        self.root = agent.SharedRoot(str(self.path), [self.secret])
        self.addCleanup(self.root.close)

    def write(self, name, content=b'new', exclusive=False, incoming=None, digest=None):
        self.root.write(name, len(content), exclusive, Socket(content if incoming is None else incoming), digest or hashlib.sha256(content).hexdigest())

    def test_unicode_read_write_and_exclusive(self):
        name = 'folder/space \u4e2d.md'
        self.write(name, b'old')
        with self.assertRaises(FileExistsError): self.write(name, b'collision', True)
        self.assertEqual((self.path/name).read_bytes(), b'old')
        self.write(name, b'updated')
        sock = Socket(); self.root.read(name, 100, sock)
        head, remaining = bytes(sock.output).split(b'\n', 1)
        self.assertEqual(json.loads(head)['size'], 7)
        self.assertEqual(remaining[:7], b'updated')
        self.assertEqual(json.loads(remaining[7:])['sha256'], hashlib.sha256(b'updated').hexdigest())

    def test_interrupted_and_corrupt_upload_preserve_original(self):
        self.write('old', b'original')
        for incoming in (b'x', b'bad-data'):
            with self.assertRaises((ConnectionError, ValueError)):
                self.write('old', b'new-data', incoming=incoming)
            self.assertEqual((self.path/'old').read_bytes(), b'original')
            self.assertFalse(list(self.path.glob('.aide-relay-*')))

    def test_root_traversal_and_protocol_paths(self):
        for name in ('../x', '/etc/passwd', 'C:/x', 'a\\x', 'a//x', 'a/./x', 'a/../x', 'a\x00x', '.aide-relay-private'):
            with self.subTest(name=name), self.assertRaises(ValueError): agent.parts(name)

    def test_script_hardlink_and_private_directory_hidden(self):
        for op in (lambda:self.root.read('agent.py', 1024, Socket()), lambda:self.write('agent.py'), lambda:self.root.mutate('remove', {'path':'agent.py'})):
            with self.assertRaises(PermissionError): op()
        os.link(self.secret, self.path/'alias')
        self.assertEqual(json.loads(self.root.listing('.')), [])
        with self.assertRaises(PermissionError): self.root.open_file('alias', {}, False)

    def test_symlink_file_and_parent_rejected(self):
        outside = tempfile.TemporaryDirectory(); self.addCleanup(outside.cleanup)
        target = Path(outside.name); (target/'private').write_bytes(b'outside')
        try:
            os.symlink(target, self.path/'link', target_is_directory=True)
            os.symlink(target/'private', self.path/'file-link')
        except OSError as error:
            self.skipTest('host does not grant symlink creation: ' + str(error))
        for name in ('link/private', 'file-link'):
            for op in (lambda:self.root.read(name,100,Socket()), lambda:self.write(name), lambda:self.root.open_file(name,{},False)):
                with self.assertRaises((OSError, ValueError)): op()
        self.assertEqual((target/'private').read_bytes(),b'outside')

    @unittest.skipUnless(hasattr(os,'mkfifo'), 'POSIX FIFO')
    def test_special_files_do_not_block(self):
        os.mkfifo(self.path/'pipe')
        with self.assertRaises(PermissionError): self.root.open_file('pipe',{},False)
        self.assertEqual(json.loads(self.root.listing('.')),[])

    def test_listing_and_header_bounds_fail_explicitly(self):
        old = agent.MAX_LIST
        agent.MAX_LIST = 2
        try:
            for name in ('a','b','c'): (self.path/name).touch()
            with self.assertRaises(ValueError): self.root.listing('.')
        finally: agent.MAX_LIST = old
        with self.assertRaises(ValueError): agent.line(Socket(b'x'*4096+b'\n'))
        with self.assertRaises(ValueError): agent.line(Socket(b'[]\n'))

    def test_atomic_no_replace_rename(self):
        self.write('a', b'a'); self.write('b',b'b')
        with self.assertRaises(FileExistsError): self.root.mutate('rename',{'path':'a','destination':'b'})
        self.assertEqual((self.path/'b').read_bytes(),b'b')
        self.root.mutate('rename',{'path':'a','destination':'c'})
        self.assertEqual((self.path/'c').read_bytes(),b'a')
        # Exercise the OS primitive independently of the preflight check.
        with self.root.directory([]) as parent:
            with self.assertRaises(FileExistsError):
                agent.rename_exclusive(self.root.filename(parent,'c'),self.root.filename(parent,'b'),parent,parent)

    def test_handle_limits_close_and_checksum_before_write(self):
        self.write('data',b'original')
        handles=agent.FileHandles(); self.addCleanup(handles.close_all)
        token,_=handles.add(self.root,{'path':'data'},True)
        with self.assertRaises(ValueError):
            agent.handle_operation('writeat',{'handle':token,'offset':0,'size':3,'sha256':'0'*64},Socket(b'bad'),self.root,handles)
        self.assertEqual((self.path/'data').read_bytes(),b'original')
        handles.close(token)
        with self.assertRaises(FileNotFoundError):
            with handles.get(token): pass
        tokens=[handles.add(self.root,{'path':'data'},False)[0] for _ in range(128)]
        with self.assertRaises(PermissionError): handles.add(self.root,{'path':'data'},False)
        handles.close_all(); self.assertFalse(handles.items)


if __name__ == '__main__':
    unittest.main()
