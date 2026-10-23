"""Offline candidate contract tests; no Docker, credentials or external services."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location('candidate', Path(__file__).with_name('build_candidate.py'))
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


def tar_bytes(files):
    data = io.BytesIO()
    with tarfile.open(fileobj=data, mode='w') as archive:
        for name, value in files.items():
            item = tarfile.TarInfo(name)
            item.size = len(value)
            archive.addfile(item, io.BytesIO(value))
    return data.getvalue()


class CandidateContract(unittest.TestCase):
    def test_version_and_native_platform(self):
        self.assertEqual(candidate.version_tag('1.2.3.4 RC12'), 'v1.2.3.4-RC12')
        for bad in ('v1.2.3.4-RC1', '1.2.3.4 RC0', '1.2.3 RC1', 'dev', '1.2.3.4 RC1;echo x'):
            with self.assertRaises(ValueError):
                candidate.version_tag(bad)
        candidate.native_platform('arm64', 'linux', 'aarch64')
        candidate.native_platform('amd64', 'linux', 'x86_64')
        with self.assertRaises(ValueError):
            candidate.native_platform('amd64', 'linux', 'aarch64')
        with self.assertRaises(ValueError):
            candidate.native_platform('arm64', 'darwin', 'arm64')

    def test_archive_reload_is_nonforced_and_rechecks_identity_before_smoke(self):
        image = 'aide:ci-deadbeef-arm64'
        identity = [{'Id': 'sha256:original', 'Os': 'linux', 'Architecture': 'arm64',
                     'Config': {'Labels': {'aide.srcsha': 'source'}}}]
        events = []
        with patch.object(candidate, 'run', side_effect=lambda args: events.append(args)), \
             patch.object(candidate, 'capture', side_effect=lambda args: events.append(args) or json.dumps(identity)), \
             patch.object(candidate, 'smoke', return_value={'status': 'pass'}) as smoke:
            result = candidate.reload_archive(image, Path('/tmp/image.tar.gz'), 'arm64',
                                              'sha256:original', 'source', 'commit', 'version')
            self.assertEqual(events, [['docker', 'image', 'rm', image],
                                      ['docker', 'load', '--input', '/tmp/image.tar.gz'],
                                      ['docker', 'image', 'inspect', image]])
            self.assertEqual(result['status'], 'pass')
            smoke.assert_called_once_with(image, 'arm64', 'commit', 'version')
        identity[0]['Id'] = 'sha256:wrong'
        with patch.object(candidate, 'run'), patch.object(candidate, 'capture', return_value=json.dumps(identity)), \
             patch.object(candidate, 'smoke') as smoke:
            with self.assertRaises(ValueError):
                candidate.reload_archive(image, Path('/tmp/image.tar.gz'), 'arm64',
                                         'sha256:original', 'source', 'commit', 'version')
            smoke.assert_not_called()
        with patch.object(candidate, 'run', side_effect=RuntimeError('image in use')) as run, \
             patch.object(candidate, 'capture') as inspect:
            with self.assertRaises(RuntimeError):
                candidate.reload_archive(image, Path('/tmp/image.tar.gz'), 'arm64',
                                         'sha256:original', 'source', 'commit', 'version')
            self.assertEqual(run.call_count, 1)
            inspect.assert_not_called()

    def test_runtime_checks_use_candidate_and_race_uses_separate_toolchain(self):
        plan = candidate.verification_plan('aide:ci-test-arm64')
        self.assertEqual(len(plan), 4)
        self.assertIn('aide:ci-test-arm64', plan[1][0])
        self.assertEqual(plan[2][1], {'AIDE_TEST_TOOLCHAIN_IMAGE': 'aide:ci-test-arm64-test-tools', 'AIDE_TEST_WORKERS': '2'})
        self.assertIn('aide:ci-test-arm64', plan[3][0])
        self.assertFalse(any('verify' in command and 'full' in command for command, _ in plan))

    def test_source_excludes_every_installed_plugin(self):
        candidate.audit_tracked(['cmd/aide/main.go', 'plugins/testdata/public.txt'])
        for name in ('plugins/official/index.js', 'plugins/private/manifest.json', '.agent-state/token', '../escape'):
            with self.assertRaises(ValueError):
                candidate.audit_tracked([name])

    def test_all_layers_are_audited_even_deleted_plugin(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'image.tar.gz'
            def write(files):
                with tarfile.open(path, 'w:gz') as outer:
                    layers = ['one/layer.tar', 'two/layer.tar']
                    payloads = {'manifest.json': json.dumps([{'Layers': layers}]).encode(),
                                layers[0]: tar_bytes(files), layers[1]: tar_bytes({'data/plugins/.wh.private': b''})}
                    for name, value in payloads.items():
                        item = tarfile.TarInfo(name); item.size = len(value)
                        outer.addfile(item, io.BytesIO(value))
            write({'usr/local/bin/aide': b'ELF'})
            # Whiteouts still reveal a previously installed plugin directory.
            with self.assertRaises(ValueError):
                candidate.audit_image_layers(path)
            with tarfile.open(path, 'w:gz') as outer:
                for name, value in {'manifest.json': b'[{"Layers":["one/layer.tar"]}]',
                                    'one/layer.tar': tar_bytes({'usr/local/bin/aide': b'ELF', 'usr/lib/krb5/plugins/preauth/library.so': b'legitimate system library'})}.items():
                    item = tarfile.TarInfo(name); item.size = len(value)
                    outer.addfile(item, io.BytesIO(value))
            self.assertEqual(candidate.audit_image_layers(path)['layers'], 1)
            for tool in ('usr/local/go/bin/go', 'usr/lib/gcc/aarch64/12/cc1',
                         'usr/local/lib/node_modules/npm/package.json', 'usr/bin/make'):
                write({tool: b'deleted-in-a-later-layer'})
                with self.assertRaisesRegex(ValueError, 'Development tool payload'):
                    candidate.audit_image_layers(path)
            write({'opt/custom/manifest.json': b'{"id":"hidden","main":"index.js"}'})
            with self.assertRaises(ValueError):
                candidate.audit_image_layers(path)

    def test_package_identity_checksums_and_data_boundaries(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'full.zip'
            tag, arch, image = 'v1.2.3.4-RC1', 'arm64', 'sha256:abc'
            root = 'aide-' + tag + '-full-linux-' + arch
            payload = b'compressed-docker-image'
            entries = {
                root + '/docker-images/manifest.json': json.dumps({'releaseTag': tag, 'platform': 'linux/' + arch, 'imageId': image, 'imageArchive': 'image.tar.gz'}),
                root + '/docker-images/SHA256SUMS': hashlib.sha256(payload).hexdigest() + '  image.tar.gz\n',
                root + '/docker-images/image.tar.gz': payload,
                root + '/.aide-image': 'aide:slot-a ' + image + ' linux/' + arch + ' ' + tag + '\n',
                root + '/workspace/': '', root + '/.env.example': 'safe=true',
            }
            for required in candidate.FULL_PACKAGE_STARTUP_FILES:
                entries.setdefault(root + '/' + required, 'startup fixture')
            def write(extra=None):
                with zipfile.ZipFile(path, 'w') as bundle:
                    for name, value in dict(entries, **(extra or {})).items():
                        bundle.writestr(name, value)
            write()
            self.assertEqual(candidate.validate_full_package(path, tag, arch, image)['status'], 'pass')
            for required in candidate.FULL_PACKAGE_STARTUP_FILES:
                removed = entries.pop(root + '/' + required)
                write()
                with self.assertRaisesRegex(ValueError, 'Missing full-package startup file'):
                    candidate.validate_full_package(path, tag, arch, image)
                entries[root + '/' + required] = removed
            write()
            with self.assertRaises(ValueError):
                candidate.validate_full_package(path, tag, arch, 'different-image')
            for name in ('workspace/private.txt', 'plugins/official/index.js', '.env', '../escape'):
                write({root + '/' + name: 'not allowed'})
                with self.assertRaises(ValueError):
                    candidate.validate_full_package(path, tag, arch, image)
            write({root + '/docker-images/image.tar.gz': b'tampered'})
            with self.assertRaises(ValueError):
                candidate.validate_full_package(path, tag, arch, image)


if __name__ == '__main__':
    unittest.main()
