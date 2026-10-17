"""Public defaults must never point at an unuploaded export."""
from pathlib import Path
import subprocess
import importlib.util
import copy
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ExportPublicationTests(unittest.TestCase):
    def test_export_records_candidate_without_changing_public_defaults(self):
        source = (ROOT / 'scripts/aide.sh').read_text()
        start = source.index('record_prebuilt_image() {')
        end = source.index('if [[ "$MODE" == source-sha', start)
        function = source[start:end]
        with tempfile.TemporaryDirectory(prefix='aide publication ') as name:
            root = Path(name)
            (root / 'docker').mkdir()
            public = root / 'docker/prebuilt-images.tsv'
            before = b'# Public defaults must remain byte-exact.\n'
            public.write_bytes(before)
            archive = root / 'fixture-offline.tar.gz'
            archive.write_bytes(b'fixture archive')
            result = subprocess.run(['bash', '-c', 'set -euo pipefail;\n' + function +
                            '\nrecord_prebuilt_image "$1" "$2" "$3" "$4" "$5"',
                            'record-test', 'linux/arm64', 'sha256:' + 'a'*64,
                            'b'*64, str(archive), 'v0.1.17.0-RC2'], cwd=root, check=False,
                           capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr.decode(errors="replace"))
            self.assertEqual(public.read_bytes(), before)
            candidate = root / '.agent-state/prebuilt-images.candidate.tsv'
            self.assertTrue(candidate.is_file())
            self.assertIn('linux/arm64 sha256:' + 'a'*64, candidate.read_text())


spec = importlib.util.spec_from_file_location('prebuilt_gate', ROOT / 'scripts/verify_prebuilt_assets.py')
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)


class PublicAssetTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / 'manifest.tsv'
        self.rows = []
        self.release = dict(tag_name='v0.1.17.0-RC2', draft=False, assets=[])
        for arch, letter in [('arm64', 'a'), ('amd64', 'c')]:
            image = letter * 64
            name = f'aide-0.1.17.0-RC2-linux-{arch}-{image[:12]}-offline.tar.gz'
            url = 'https://github.com/skyelan1999/aide/releases/download/v0.1.17.0-RC2/' + name
            self.rows.append(f'linux/{arch} sha256:{image} {"b"*64} {"d"*64} {url} bundle')
            self.release['assets'].append(dict(name=name, state='uploaded', size=123,
                                               digest='sha256:'+'d'*64, browser_download_url=url))
        self.write()

    def write(self):
        self.path.write_text('\n'.join(self.rows)+'\n')

    def check(self, release=None):
        return gate.verify(self.path, fetch=lambda tag: self.release if release is None else release)

    def test_matching_public_pair_passes_without_writing(self):
        before = self.path.read_bytes()
        result = self.check()
        self.assertEqual(result['status'], 'pass')
        self.assertEqual(len(result['assets']), 2)
        self.assertFalse(result['mutations'])
        self.assertEqual(self.path.read_bytes(), before)

    def test_missing_duplicate_draft_or_wrong_release_fail(self):
        cases = []
        for kind in ['missing', 'duplicate', 'draft', 'wrong-tag']:
            release = copy.deepcopy(self.release)
            if kind == 'missing': release['assets'].pop()
            elif kind == 'duplicate': release['assets'].append(release['assets'][0])
            elif kind == 'draft': release['draft'] = True
            else: release['tag_name'] = 'v-unrelated'
            cases.append(release)
        for release in cases:
            with self.subTest(release=release['tag_name']), self.assertRaises(ValueError):
                self.check(release)

    def test_public_metadata_must_match_digest_url_size_and_upload_state(self):
        for field, value in [('digest', None), ('digest', 'sha256:'+'e'*64),
                             ('browser_download_url', 'https://elsewhere.invalid/image.tar.gz'),
                             ('size', 0), ('size', True), ('state', 'new')]:
            release = copy.deepcopy(self.release)
            release['assets'][0][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                self.check(release)

    def test_incomplete_or_mixed_source_pair_fails_before_network(self):
        variants = [self.rows[:1], [self.rows[0], self.rows[0]],
                    [self.rows[0], self.rows[1].replace('b'*64, 'e'*64)]]
        for rows in variants:
            self.path.write_text('\n'.join(rows)+'\n')
            with self.assertRaises(ValueError):
                gate.verify(self.path, fetch=lambda _: self.fail('network reached before validation'))

    def test_untrusted_url_or_bad_identity_is_rejected_without_echoing_secrets(self):
        original = self.rows[0]
        for bad in [original.replace('https://github.com/', 'https://user:synthetic-secret@github.com/'),
                    original.replace('bundle', 'image'), original.replace('a'*64, 'bad-image')]:
            self.path.write_text(bad+'\n'+self.rows[1]+'\n')
            with self.assertRaises(ValueError) as error:
                self.check()
            self.assertNotIn('synthetic-secret', str(error.exception))

    def test_network_failure_propagates_without_changing_manifest(self):
        before = self.path.read_bytes()
        def offline(_): raise OSError('fixture offline')
        with self.assertRaises(OSError): gate.verify(self.path, fetch=offline)
        self.assertEqual(self.path.read_bytes(), before)


if __name__ == '__main__':
    unittest.main()
