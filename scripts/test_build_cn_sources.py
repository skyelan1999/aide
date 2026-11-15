#!/usr/bin/env python3
"""Execute real recovery wrappers and shared POSIX/native planning contracts."""
import contextlib
import io
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from test_launcher_contract import BoundaryLauncher, module

ROOT = Path(__file__).resolve().parents[1]
DEFAULTS = (ROOT / 'Dockerfile').read_text()
FAILURE = 'ERROR: failed to solve: python:3.12-slim-bookworm: failed to resolve source metadata for docker.io/library/python:3.12-slim-bookworm: failed to do request: timeout'


class SourcePlanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='aide-cn-plan-')
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.binary = Path(cls.temporary.name) / 'planner'
        subprocess.run(['c++', '-std=c++17', '-O0', str(ROOT / 'scripts/native_launcher_portable.cpp'),
                        str(ROOT / 'native/windows-launcher/third_party/miniz/miniz_tinfl.c'),
                        '-o', str(cls.binary)], check=True, capture_output=True)

    def test_python_and_real_native_planner_agree(self):
        digest = 'a' * 64
        cases = [
            (FAILURE, {}, {'PYTHON_BASE': 'docker.m.daocloud.io/library/python:3.12-slim-bookworm'}),
            (FAILURE, {'AIDE_CN_SOURCE_FALLBACK': '0'}, {}),
            (FAILURE, {'PYTHON_BASE': 'private.example/team/python:3.12'}, {}),
            (FAILURE, {'PYTHON_BASE': 'aide:local-base'}, {}),
            (FAILURE, {'PYTHON_BASE': ''}, {}),
            ('ERROR: go build: syntax error: unexpected timeout', {}, {}),
            ('ERROR: apt-get update: network timeout', {}, {}),
            ('ERROR: no space left on device', {}, {}),
            ('ERROR: failed to resolve source metadata for aide:dependencies-v2', {}, {}),
            ('ERROR: python:3.12: failed to fetch anonymous token: 403', {'PYTHON_BASE': 'python:3.12'},
             {'PYTHON_BASE': 'docker.m.daocloud.io/library/python:3.12'}),
            ('ERROR: docker.io/library/node:24@sha256:' + digest + ': failed to resolve source metadata',
             {'NODE_BASE': 'docker.io/library/node:24@sha256:' + digest},
             {'NODE_BASE': 'docker.m.daocloud.io/library/node:24@sha256:' + digest}),
        ]
        for output, arguments, expected in cases:
            with self.subTest(output=output, arguments=arguments):
                self.assertEqual(module.mainland_base_arguments(output, DEFAULTS, arguments), expected)
                result = subprocess.run([str(self.binary), 'build-sources'],
                    input=json.dumps(dict(output=output, dockerfile=DEFAULTS, arguments=arguments)),
                    capture_output=True, text=True, check=True)
                self.assertEqual(json.loads(result.stdout), expected)

    def test_streamed_diagnostics_and_timeout(self):
        with tempfile.TemporaryDirectory() as directory:
            launcher = module.Launcher(Path(directory))
            output = io.StringIO()
            with contextlib.redirect_stdout(output), self.assertRaises(module.BuildFailure) as caught:
                launcher.stream_build([sys.executable, '-c', 'import sys; print("ERROR: fixture timeout",flush=True);sys.exit(29)'], 5)
            self.assertIn('fixture timeout', caught.exception.output)
            self.assertIn('fixture timeout', output.getvalue())
            with self.assertRaises(subprocess.TimeoutExpired):
                launcher.stream_build([sys.executable, '-c', 'import time;time.sleep(30)'], .2)

    def test_failed_retry_keeps_configuration_and_never_starts(self):
        for permanent in (False, True):
            with self.subTest(permanent=permanent), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / '.git').mkdir()
                (root / 'Dockerfile').write_text(DEFAULTS)
                (root / 'version.md').write_text('0.1.17.6 RC1')
                (root / '.env').write_text('AIDE_IMAGE=aide:previous\n')
                launcher = BoundaryLauncher(root)
                original = launcher.call
                attempts = []
                def fail_once(args, **kwargs):
                    if args[1:3] == ['buildx', 'build']:
                        attempts.append(args)
                        if len(attempts) == 1 or permanent:
                            raise module.BuildFailure(1, FAILURE)
                    return original(args, **kwargs)
                with patch.object(launcher, 'call', side_effect=fail_once):
                    if permanent:
                        with self.assertRaises(module.BuildFailure): launcher.build()
                    else:
                        launcher.build()
                self.assertEqual(len(attempts), 2)
                self.assertIn('PYTHON_BASE=docker.m.daocloud.io/library/python:3.12-slim-bookworm', attempts[-1])
                started = ['bash', 'scripts/aide.sh', 'start-built'] in launcher.commands
                self.assertEqual(started, not permanent)
                if permanent:
                    self.assertEqual((root / '.env').read_text(), 'AIDE_IMAGE=aide:previous\n')
                    self.assertEqual(launcher.recycle_calls, [])


    def test_independent_base_failures_each_switch_once(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / '.git').mkdir()
            (root / 'Dockerfile').write_text(DEFAULTS)
            (root / 'version.md').write_text('0.1.17.6 RC1')
            (root / '.env.example').write_text('AIDE_IMAGE=aide:previous\n')
            launcher = BoundaryLauncher(root); original = launcher.call; attempts = []
            failures = [FAILURE,
                'ERROR: node:24-bookworm-slim: failed to resolve source metadata',
                'ERROR: golang:1.26-bookworm: failed to resolve source metadata']
            def fail_bases(args, **kwargs):
                if args[1:3] == ['buildx', 'build']:
                    attempts.append(args)
                    if len(attempts) <= 3: raise module.BuildFailure(1, failures[len(attempts)-1])
                return original(args, **kwargs)
            with patch.object(launcher, 'call', side_effect=fail_bases): launcher.build()
            self.assertEqual(len(attempts), 4)
            for key in ('PYTHON_BASE', 'NODE_BASE', 'GO_BASE'):
                self.assertEqual(sum(a.startswith(key + '=docker.m.daocloud.io/') for a in attempts[-1]), 1)



class DependencyWrapperTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='aide-cn-deps-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.log = self.root / 'calls.json'
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ['PATH'],
                        ATTEMPT_LOG=str(self.log), FAILURES='1', FAILURE_TEXT='Connection reset by peer',
                        PIP_INDEX_URL='https://mirrors.aliyun.com/pypi/simple/')
        for key in ('PIP_NO_INDEX', 'PIP_EXTRA_INDEX_URL', 'AIDE_CN_SOURCE_FALLBACK'):
            self.env.pop(key, None)
        command = self.root / 'fixture'
        command.write_text('#!' + sys.executable + '\n' + '''import json,os,pathlib,sys
p=pathlib.Path(os.environ['ATTEMPT_LOG']);entries=json.loads(p.read_text()) if p.exists() else []
entries.append(sys.argv[1:]);p.write_text(json.dumps(entries))
failed=len(entries)<=int(os.environ['FAILURES'])
if failed:print(os.environ['FAILURE_TEXT'],file=sys.stderr)
sys.exit(23 if failed else 0)
''')
        command.chmod(0o700)
        shutil.copy2(command, self.root / 'apt-get')
        sleep = self.root / 'sleep'; sleep.write_text('#!/bin/sh\nexit 0\n'); sleep.chmod(0o700)

    def run_pip(self, index='https://mirrors.aliyun.com/pypi/simple/', options=()):
        args = ['install', '-i', index, *options, 'python-docx==1.2.0', 'pypdf[crypto]==6.19.0']
        result = subprocess.run(['sh', str(ROOT / 'scripts/pip-install-retry.sh'), str(self.root / 'fixture'), *args],
                                env=self.env, capture_output=True, text=True, timeout=10)
        return result, json.loads(self.log.read_text()), args

    def test_pip_network_failure_switches_index_and_preserves_pins(self):
        result, calls, args = self.run_pip()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)
        self.assertEqual(calls[1][2], 'https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple/')
        self.assertEqual(calls[1][3:], args[3:])

    def test_pip_two_mirrors_then_exit_original_status(self):
        self.env['FAILURES'] = '99'
        result, calls, _ = self.run_pip()
        self.assertEqual(result.returncode, 23)
        self.assertEqual(len(calls), 3)
        self.assertEqual(calls[2][2], 'https://mirrors.huaweicloud.com/repository/pypi/simple/')

    def test_pip_offline_private_extra_index_and_optout_never_switch(self):
        cases = [('https://private.example/simple/', (), {}),
                 ('https://mirrors.aliyun.com/pypi/simple/', ('--no-index',), {}),
                 ('https://mirrors.aliyun.com/pypi/simple/', ('--extra-index-url', 'https://private.example/simple/'), {}),
                 ('https://mirrors.aliyun.com/pypi/simple/', (), {'AIDE_CN_SOURCE_FALLBACK': '0'})]
        for index, options, env in cases:
            with self.subTest(index=index, options=options, env=env):
                self.log.unlink(missing_ok=True)
                self.env.pop('AIDE_CN_SOURCE_FALLBACK', None); self.env.update(env)
                result, calls, args = self.run_pip(index, options)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(calls, [args, args])

    def test_pip_package_error_does_not_switch(self):
        self.env['FAILURE_TEXT'] = 'ERROR: No matching distribution found for imaginary==999'
        result, calls, args = self.run_pip()
        self.assertEqual(calls, [args, args])

    def run_apt(self, text, failure='Failed to fetch: Connection timed out', failures='1', disabled=False):
        directory = self.root / 'apt/sources.list.d'; directory.mkdir(parents=True, exist_ok=True)
        source = directory / 'debian.sources'; source.write_text(text)
        self.env.update(AIDE_APT_SOURCES_DIR=str(directory.parent), FAILURE_TEXT=failure, FAILURES=failures)
        if disabled: self.env['AIDE_CN_SOURCE_FALLBACK'] = '0'
        result = subprocess.run(['sh', str(ROOT / 'scripts/apt-install-retry.sh'), 'curl=1.0'],
                                env=self.env, capture_output=True, text=True, timeout=10)
        return result, json.loads(self.log.read_text()), source.read_text()

    def test_deb822_mirror_preserves_suites_keys_and_package_pins(self):
        source = 'URIs: http://deb.debian.org/debian\nSuites: bookworm bookworm-updates\nSigned-By: /keys/debian.gpg\n\nURIs: http://deb.debian.org/debian-security\nSuites: bookworm-security\n'
        result, calls, modified = self.run_apt(source)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('https://mirrors.tuna.tsinghua.edu.cn/debian-security', modified)
        self.assertIn('Suites: bookworm bookworm-updates', modified)
        self.assertIn('Signed-By: /keys/debian.gpg', modified)
        self.assertEqual(calls[-1][-1], 'curl=1.0')
        self.assertEqual(len(calls), 3)

    def test_apt_permanent_failure_is_bounded(self):
        result, calls, modified = self.run_apt('deb http://deb.debian.org/debian bookworm main\n', failures='99')
        self.assertEqual(result.returncode, 23)
        self.assertEqual(len(calls), 3)
        self.assertIn('https://mirrors.aliyun.com/debian', modified)

    def test_apt_custom_source_package_error_and_optout_never_switch(self):
        for source, failure, disabled in [('deb https://private.example/debian bookworm main\n', 'Failed to fetch: timeout', False),
            ('deb http://deb.debian.org/debian bookworm main\n', 'Unable to locate package', False),
            ('deb http://deb.debian.org/debian bookworm main\n', 'Failed to fetch: timeout', True)]:
            with self.subTest(source=source, failure=failure, disabled=disabled):
                self.log.unlink(missing_ok=True)
                result, calls, modified = self.run_apt(source, failure, disabled=disabled)
                self.assertEqual(result.returncode, 23)
                self.assertEqual(modified, source)
                self.assertEqual(len(calls), 1)


    def test_gnu_download_fallback_retains_checksum_and_rejects_corruption(self):
        for disabled, corrupt, expected in [(False, False, 37), (False, True, 1), (True, False, 28)]:
            with self.subTest(disabled=disabled, corrupt=corrupt), tempfile.TemporaryDirectory() as d:
                base = Path(d); payload = b'pinned-source-fixture'
                metadata = base / 'source.json'
                metadata.write_text(json.dumps(dict(version='0.14', url='https://ftp.gnu.org/gnu/libredwg/libredwg-0.14.tar.xz', sha256=hashlib.sha256(payload).hexdigest())))
                curl = base / 'curl'
                curl.write_text('#!' + sys.executable + '\n' + r'''import os,pathlib,sys
p=pathlib.Path(os.environ['URL_LOG']);urls=p.read_text().splitlines() if p.exists() else []
url=next(a for a in sys.argv if a.startswith('https://'));p.write_text('\n'.join(urls+[url]))
if len(urls)==0:sys.exit(28)
pathlib.Path(sys.argv[sys.argv.index('-o')+1]).write_bytes(b'corrupt' if os.environ['CORRUPT']=='1' else b'pinned-source-fixture')
''')
                curl.chmod(0o700)
                tar = base / 'tar'; tar.write_text('#!/bin/sh\nexit 37\n'); tar.chmod(0o700)
                env = dict(self.env, PATH=str(base)+os.pathsep+self.env['PATH'], AIDE_DWG_SOURCE_CACHE=str(base/'cache'),
                           URL_LOG=str(base/'urls'), CORRUPT='1' if corrupt else '0', AIDE_CN_SOURCE_FALLBACK='0' if disabled else '1')
                result = subprocess.run(['sh', str(ROOT/'scripts/build-dwg-converter.sh'), str(base/'output'), str(metadata)],
                                        env=env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, expected, result.stderr)
                urls = (base/'urls').read_text().splitlines()
                self.assertEqual(len(urls), 1 if disabled else 2)
                if not disabled: self.assertEqual(urls[-1], 'https://mirrors.tuna.tsinghua.edu.cn/gnu/libredwg/libredwg-0.14.tar.xz')
                self.assertEqual((base/'cache/libredwg-0.14.tar.xz').exists(), not disabled and not corrupt)



if __name__ == '__main__': unittest.main()
