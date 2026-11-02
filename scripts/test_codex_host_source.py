#!/usr/bin/env python3
"""Pinned source preparation: real local HTTP faults and offline cache reuse."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('codex_host_source', ROOT / 'scripts/build-codex-host.py')
source = importlib.util.module_from_spec(spec)
spec.loader.exec_module(source)


class SourceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='aide-source-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.data = b'owned synthetic source archive'
        self.lock = {'version': '1.2.3', 'commit': 'a' * 40,
                     'sha256': hashlib.sha256(self.data).hexdigest()}
        self.cache = self.root / 'cache'
        self.statuses = [200]
        self.hits = 0
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                owner.hits += 1
                status = owner.statuses.pop(0) if owner.statuses else 200
                self.send_response(status)
                self.end_headers()
                self.wfile.write(owner.data)

            def log_message(self, *_):
                pass

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(lambda: (server.shutdown(), server.server_close(), thread.join()))
        self.http = 'http://127.0.0.1:%d/source' % server.server_port
        self.urlopen = source.urllib.request.urlopen

    def local_urlopen(self, url, **kwargs):
        self.assertTrue(url.startswith('https://codeload.github.com/skyelan1999/aide-pkg-CodexBridge/tar.gz/'))
        return self.urlopen(self.http, **kwargs)

    def prepare(self):
        with patch.object(source.urllib.request, 'urlopen', self.local_urlopen):
            return source.prepare_source(self.lock, cache_dir=self.cache)

    def test_cache_reuse_is_offline_and_every_use_is_verified(self):
        archive = self.prepare()
        self.assertEqual(archive.read_bytes(), self.data)
        self.assertEqual(self.hits, 1)
        bundle = self.root / 'bundle' / archive.name
        source.verified_copy(archive, bundle, self.lock['sha256'])
        with patch.object(source.urllib.request, 'urlopen', side_effect=AssertionError('unexpected network')):
            self.assertEqual(source.prepare_source(self.lock, cache_dir=self.cache), archive)
            self.assertEqual(source.prepare_source(self.lock, cache_dir=self.root/'empty', bundle_dir=bundle.parent), bundle)
            bundle.write_bytes(b'wrong source')
            with self.assertRaisesRegex(ValueError, 'checksum'):
                source.prepare_source(self.lock, cache_dir=self.cache, bundle_dir=bundle.parent)

    def test_transient_http_retries_then_commits_one_verified_archive(self):
        self.statuses = [503, 429, 200]
        with patch.object(source.time, 'sleep'):
            self.prepare()
        self.assertEqual(self.hits, 3)
        self.assertEqual([p.name for p in self.cache.iterdir()], [self.lock['sha256']+'.tar.gz'])

    def test_retry_exhaustion_is_bounded_and_retains_no_partial_archive(self):
        with patch.object(source.urllib.request, 'urlopen', side_effect=urllib.error.URLError(ConnectionRefusedError())) as fetch, patch.object(source.time, 'sleep'):
            with self.assertRaisesRegex(RuntimeError, 'after 3 attempts'):
                source.prepare_source(self.lock, cache_dir=self.cache)
            self.assertEqual(fetch.call_count, 3)
        self.assertEqual(list(self.cache.iterdir()), [])

    def test_permanent_http_and_bad_checksum_are_not_retried(self):
        self.statuses = [404]
        with self.assertRaisesRegex(RuntimeError, 'HTTP 404'):
            self.prepare()
        self.assertEqual(self.hits, 1)
        self.data = b'incorrect archive'
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.prepare()
        self.assertEqual(self.hits, 2)
        self.assertEqual(list(self.cache.iterdir()), [])

    def test_corrupt_download_cache_is_repaired_only_by_verified_download(self):
        self.cache.mkdir()
        bad = self.cache / (self.lock['sha256'] + '.tar.gz')
        bad.write_bytes(b'corrupt')
        self.assertEqual(self.prepare().read_bytes(), self.data)
        bad.write_bytes(b'corrupt again')
        self.data = b'bad response'
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertEqual(bad.read_bytes(), b'corrupt again')
        self.assertEqual(len(list(self.cache.iterdir())), 1)

    def test_size_limit_explicit_archive_and_symlink_boundaries(self):
        with patch.object(source, 'MAX_SOURCE', 4):
            with self.assertRaisesRegex(ValueError, 'too large'):
                self.prepare()
        self.assertEqual(list(self.cache.iterdir()), [])
        wrong = self.root / 'wrong.tar.gz'
        wrong.write_bytes(b'wrong')
        with patch.object(source.urllib.request, 'urlopen', side_effect=AssertionError('unexpected network')):
            with self.assertRaisesRegex(ValueError, 'checksum'):
                source.prepare_source(self.lock, archive=wrong)
        link = self.cache / (self.lock['sha256'] + '.tar.gz')
        try:
            link.symlink_to(wrong)
        except OSError:
            return  # Link creation is not available to ordinary Windows accounts.
        with self.assertRaisesRegex(ValueError, 'symbolic link'):
            self.prepare()

    def test_prepare_only_works_without_go_or_node_and_creates_no_binaries(self):
        archive = self.root / 'source.tar.gz'
        archive.write_bytes(self.data)
        pin = self.root / 'pin.json'
        pin.write_text(json.dumps(self.lock))
        bundle = self.root / 'bundle with spaces'
        result = subprocess.run([sys.executable, str(ROOT/'scripts/build-codex-host.py'), '--prepare-only',
                                 '--pin', str(pin), '--archive', str(archive), '--bundle-dir', str(bundle)],
                                capture_output=True, text=True, check=True)
        self.assertIn('SHA-256 verified', result.stdout)
        self.assertEqual((bundle/(self.lock['sha256']+'.tar.gz')).read_bytes(), self.data)
        self.assertEqual(len(list(bundle.iterdir())), 1)

    def test_docker_host_stage_is_independent_of_application_inputs(self):
        for file in ['Dockerfile', 'Dockerfile.local']:
            text = (ROOT/file).read_text()
            stage = text.split(' AS codex-host\n', 1)[1].split('\nFROM ', 1)[0]
            self.assertNotIn('COPY internal', stage)
            self.assertNotIn('COPY cmd', stage)
            self.assertIn('COPY docker/host-sources/', stage)
            self.assertIn('target=/root/.cache/aide/codex-host-sources', stage)
            self.assertIn('COPY --from=codex-host /opt/aide-host-components', text)


if __name__ == '__main__':
    unittest.main()
