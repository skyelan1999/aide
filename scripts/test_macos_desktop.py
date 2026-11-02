#!/usr/bin/env python3
"""Desktop bootstrap/ownership regressions; no real credentials or Docker used."""
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace('-', '_'), ROOT / ('scripts/' + name + '.py'))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


session = load('desktop-session')
builder = load('build-macos-desktop')


class DesktopTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='aide-desktop-中文 空格-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        (self.root / 'scripts').mkdir()
        (self.root / 'scripts/aide.sh').write_text('# fixture')
        (self.root / 'compose.yaml').write_text('services: {}')
        self.cert = self.root / '.aide-data/storage/data/certs/cert.pem'
        self.cert.parent.mkdir(parents=True)
        self.cert.write_text('-----BEGIN CERTIFICATE-----\nZmFrZQ==\n-----END CERTIFICATE-----\n')
        self.calls = []
        self.foreign = False
        self.bad_token = False

    def command(self, root, command):
        self.calls.append(command)
        self.assertEqual(root, self.root)
        args = command[1:]
        if args[:2] == ['compose', 'ps']:
            return 'a' * 64
        if args[0] == 'inspect':
            return json.dumps([{'State': {'Running': True}, 'Config': {'Labels': {
                'com.docker.compose.project.working_dir': '/foreign' if self.foreign else str(self.root)}}}])
        if args[:2] == ['compose', 'port']:
            return '0.0.0.0:9999\n[::]:9999'
        if args[0] == 'exec' and 'curl' in args:
            return json.dumps({'service': 'aide', 'status': 'ok', 'instanceId': 'fixture-instance'})
        if args[0] == 'exec' and 'sh' in args:
            return '#bad token' if self.bad_token else 'f' * 64
        raise AssertionError(command)

    def health(self, origin, certificate):
        self.assertEqual(origin, 'https://localhost:9999')
        self.assertEqual(certificate, self.cert)
        return {'service': 'aide', 'status': 'ok', 'instanceId': 'fixture-instance'}

    def test_verified_session_uses_the_current_installation_and_cert(self):
        value = session.session(self.root, command=self.command, health=self.health)
        self.assertEqual(value['origin'], 'https://localhost:9999')
        self.assertEqual(value['certificateSHA256'], hashlib.sha256(b'fake').hexdigest())
        self.assertEqual(value['token'], 'f' * 64)

    def test_foreign_owner_and_identity_mismatch_never_read_token(self):
        self.foreign = True
        with self.assertRaisesRegex(ValueError, 'another installation'):
            session.session(self.root, command=self.command, health=self.health)
        self.assertFalse(any('sh' in call for call in self.calls))
        self.calls.clear()
        self.foreign = False
        with self.assertRaisesRegex(ValueError, 'identity'):
            session.session(self.root, command=self.command, health=lambda *args: {
                'service': 'aide', 'status': 'ok', 'instanceId': 'another-instance'})
        self.assertFalse(any('sh' in call for call in self.calls))

    def test_invalid_certificate_link_and_token_are_rejected(self):
        actual = self.cert.with_name('other.pem')
        self.cert.rename(actual)
        self.cert.symlink_to(actual)
        with self.assertRaisesRegex(ValueError, 'certificate'):
            session.session(self.root, command=self.command, health=self.health)
        self.cert.unlink()
        actual.rename(self.cert)
        self.bad_token = True
        with self.assertRaisesRegex(ValueError, 'token'):
            session.session(self.root, command=self.command, health=self.health)

    def test_ports_require_one_valid_mapping(self):
        for value in ('', 'localhost:9999', '0.0.0.0:0', '[::]:65536',
                      '0.0.0.0:9999\n[::]:9998', 'https://remote.test:9999'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                session.published_port(value)
        self.assertEqual(session.published_port('127.0.0.1:9999\n[::1]:9999'), 9999)

    def test_system_curl_keeps_tls_and_proxy_redirect_boundaries(self):
        response = subprocess.CompletedProcess([], 0, b'{"service":"aide","status":"ok"}', b'')
        with patch.object(session.subprocess, 'run', return_value=response) as call:
            self.assertEqual(session.host_health('https://localhost:9999', self.cert)['status'], 'ok')
        command = call.call_args.args[0]
        self.assertEqual(command[:2], ['curl', '--disable'])
        for required in ('--cacert', '--proto', '=https', '--noproxy', '--max-time', '--max-filesize'):
            self.assertIn(required, command)
        for forbidden in ('-k', '--insecure', '-L', '--location'):
            self.assertNotIn(forbidden, command)
        with patch.object(session.subprocess, 'run', return_value=subprocess.CompletedProcess([], 60, b'', b'error')):
            with self.assertRaises(ValueError):
                session.host_health('https://localhost:9999', self.cert)

    def test_failed_start_is_redacted_and_does_not_query_credentials(self):
        output = 'URL #token=private-local-token\napi_key=sk-privatefixture\npassword=private-password\n'
        with patch.object(session.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, output, '')) as call:
            with self.assertRaisesRegex(RuntimeError, 'startup failed'):
                session.session(self.root, start=True, command=self.command)
        log = (self.root / '.aide-data/desktop/startup.log').read_text()
        for private in ('private-local-token', 'sk-privatefixture', 'private-password'):
            self.assertNotIn(private, log)
        self.assertEqual(self.calls, [])
        self.assertEqual(call.call_args.kwargs['env']['AIDE_OPEN_BROWSER'], '0')

    def test_app_identity_is_scoped_and_foreign_install_is_preserved(self):
        app = self.root / 'Aide.app'
        (app / 'Contents/MacOS').mkdir(parents=True)
        (app / 'Contents/MacOS/AideDesktop').write_bytes(b'fixture')
        info = {'CFBundleIdentifier': builder.bundle_id(self.root), 'AideProjectRoot': str(self.root)}
        (app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
        self.assertTrue(builder.owned_bundle(app, self.root))
        self.assertFalse(builder.owned_bundle(app, self.root / 'other-project'))
        info['AideProjectRoot'] = '/foreign'
        (app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
        with self.assertRaisesRegex(ValueError, 'another installation'):
            builder.install_app(app, self.root, app)
        self.assertEqual(plistlib.loads((app / 'Contents/Info.plist').read_bytes()), info)

    def test_open_existing_never_compiles_or_installs(self):
        app = self.root / 'compiled/Aide.app'
        with patch.object(sys, 'argv', ['build-macos-desktop.py', '--root', str(self.root), '--open-existing']), \
                patch.object(builder, 'current_app', return_value=app), \
                patch.object(builder, 'compile_app', side_effect=AssertionError('must not compile')), \
                patch.object(builder, 'install_app', side_effect=AssertionError('must not install')), \
                patch.object(builder, 'verified_app', side_effect=lambda app: app), \
                patch.object(builder, 'installed_current', side_effect=lambda app, root: app), \
                patch.object(builder.subprocess, 'run') as command, contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(builder.main(), 0)
        self.assertEqual(command.call_args.args[0], ['open', '-a', str(app), '--args', '--root', str(self.root)])
        with patch.object(sys, 'argv', ['build-macos-desktop.py', '--root', str(self.root), '--open-existing']), \
                patch.object(builder, 'current_app', return_value=None), patch.object(builder.subprocess, 'run') as command:
            self.assertEqual(builder.main(), 3)
        command.assert_not_called()

    def test_recycle_preserves_running_current_foreign_and_unknown_files(self):
        builds = self.root / '.aide-data/desktop/builds'
        def fixture(digest, root=None):
            app = builds / (digest * 64) / 'Aide.app'
            (app / 'Contents/MacOS').mkdir(parents=True)
            (app / 'Contents/MacOS/AideDesktop').write_bytes(b'fixture')
            (app / 'Contents/Info.plist').write_bytes(plistlib.dumps({
                'CFBundleIdentifier': builder.bundle_id(root or self.root),
                'AideProjectRoot': str(root or self.root)}))
            return app
        current = fixture('a')
        obsolete = fixture('b')
        running = fixture('c')
        foreign = fixture('d', self.root / 'foreign')
        unknown = fixture('e')
        (unknown.parent / 'user-notes.txt').write_text('preserve')
        removed = builder.recycle_builds(self.root, current, [str(running / 'Contents/MacOS/AideDesktop')])
        self.assertEqual(removed, ['b' * 64])
        self.assertFalse(obsolete.exists())
        for preserved in (current, running, foreign, unknown):
            self.assertTrue(preserved.exists())

    def test_installed_app_survives_cache_eviction_and_failed_install_keeps_receipt(self):
        home = self.root / 'home'
        app = home / 'Applications/Aide.app'
        (app / 'Contents/MacOS').mkdir(parents=True)
        (app / 'Contents/MacOS/AideDesktop').write_bytes(b'fixture')
        digest = 'a' * 64
        info = {'CFBundleIdentifier': builder.bundle_id(self.root), 'AideProjectRoot': str(self.root), 'AideDesktopBuildDigest': digest}
        (app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
        directory = self.root / '.aide-data/desktop'
        directory.mkdir()
        receipt = directory / 'current.json'
        receipt.write_text(json.dumps({'digest': digest}))
        original = receipt.read_bytes()
        with patch.object(Path, 'home', return_value=home):
            self.assertEqual(builder.current_app(self.root), app)
            self.assertFalse(builder.build_directory(self.root).exists())
            with patch.object(sys, 'argv', ['build-macos-desktop.py', '--root', str(self.root), '--install']), \
                    patch.object(builder, 'compile_app', return_value=app), \
                    patch.object(builder, 'verified_app', side_effect=lambda app: app), \
                    patch.object(builder, 'install_app', side_effect=OSError('fixture install failure')), \
                    contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(builder.main(), 1)
        self.assertEqual(receipt.read_bytes(), original)

    def test_receipt_traversal_and_symlinks_are_rejected(self):
        directory = self.root / '.aide-data/desktop'
        directory.mkdir()
        (directory / 'current.json').write_text('{"digest":"../../outside"}')
        with self.assertRaisesRegex(ValueError, 'identity'):
            builder.current_app(self.root)
        (directory / 'current.json').unlink()
        (directory / 'current.json').symlink_to(self.root / 'compose.yaml')
        with self.assertRaisesRegex(ValueError, 'symbolic'):
            builder.current_app(self.root)


if __name__ == '__main__':
    unittest.main()
