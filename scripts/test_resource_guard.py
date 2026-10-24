#!/usr/bin/env python3
"""Exercise launcher safety boundaries without starting a real Docker engine."""
import os
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]


class ResourceGuardTests(unittest.TestCase):
    def run_guard(self, available, phase='start', memory=6 << 30):
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            for exe, source in {
                'df': '#!/bin/sh\nprintf "Filesystem 1024-blocks Used Available Capacity Mounted\\nfixture 99999999 0 %s 1%% /\\n" "$FREE_KIB"\n',
                'docker': '#!/bin/sh\ncase "$*" in *MemTotal*) echo "$MEM_BYTES";; *NCPU*) echo 4;; *) exit 99;; esac\n',
            }.items():
                path = directory / exe
                path.write_text(source)
                path.chmod(0o700)
            env = dict(os.environ, PATH=str(directory)+os.pathsep+os.environ['PATH'],
                       FREE_KIB=str(available), MEM_BYTES=str(memory), PROJECT_DIR=name,
                       DOCKER_BIN=str(directory/'docker'))
            return subprocess.run(['bash', '-c', 'set -euo pipefail; source "$1"; aide_check_disk "$2"; aide_check_engine_resources; echo MUTATION_ALLOWED',
                                   'guard-test', str(ROOT/'scripts/resource-guard.sh'), phase],
                                  env=env, capture_output=True, text=True)

    def test_low_disk_stops_before_mutation(self):
        result = self.run_guard(10 * 1048576 - 1)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('MUTATION_ALLOWED', result.stdout)
        self.assertIn('insufficient disk', result.stderr)

    def test_build_reserves_more_space_than_import(self):
        self.assertEqual(self.run_guard(15 * 1048576).returncode, 0)
        self.assertNotEqual(self.run_guard(15 * 1048576, 'build').returncode, 0)

    def test_low_engine_memory_stops_before_mutation(self):
        result = self.run_guard(30 * 1048576, memory=2 << 30)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('MUTATION_ALLOWED', result.stdout)

    def test_unknown_disk_and_engine_reports_fail_closed(self):
        self.assertNotEqual(self.run_guard('unknown').returncode, 0)
        self.assertNotEqual(self.run_guard(30 * 1048576, memory='unknown').returncode, 0)

    def test_real_launcher_rejects_low_disk_before_migration_or_docker(self):
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            scripts = directory / 'scripts'
            scripts.mkdir()
            for filename in ('aide.sh', 'resource-guard.sh'):
                (scripts / filename).write_bytes((ROOT / 'scripts' / filename).read_bytes())
            # Both must remain unreachable when disk is exhausted.
            (scripts / 'launcher-data.sh').write_text('echo MIGRATION_REACHED; exit 97\n')
            for exe, body in {'df': 'printf "fixture 100 99 1 99%% /\\n"',
                              'docker': 'echo DOCKER_REACHED; exit 98'}.items():
                path = directory / exe
                path.write_text('#!/bin/sh\n' + body + '\n')
                path.chmod(0o700)
            env = dict(os.environ, PATH=str(directory)+os.pathsep+os.environ['PATH'])
            for mode in ('start', 'start-image', 'start-bundle'):
                result = subprocess.run(['bash', str(scripts / 'aide.sh'), mode],
                                        env=env, capture_output=True, text=True, timeout=10)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('insufficient disk', result.stderr)
                self.assertNotIn('MIGRATION_REACHED', result.stdout)
                self.assertNotIn('DOCKER_REACHED', result.stdout)

    def test_snapshot_launch_is_bounded_and_keeps_sharded_command(self):
        spec = importlib.util.spec_from_file_location('bounded_snapshot', ROOT / 'scripts/test_go_snapshot.py')
        snapshot = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(snapshot)
        with tempfile.TemporaryDirectory() as name:
            snapshot.ROOT = Path(name)
            with mock.patch.dict(os.environ, {'AIDE_TEST_WORKERS': '1'}), \
                 mock.patch.object(snapshot.subprocess, 'check_output', return_value=b''), \
                 mock.patch.object(snapshot.subprocess, 'run') as run:
                snapshot.main()
            args = run.call_args_list[0].args[0]
            for flag, value in (('--memory', '4g'), ('--memory-swap', '4g'),
                                ('--cpus', '2'), ('--pids-limit', '512'), ('--network', 'none')):
                self.assertEqual(args[args.index(flag)+1], value)
            self.assertEqual(run.call_args_list[0].kwargs['timeout'], snapshot.HOST_TIMEOUT_SECONDS)
            self.assertIn(f'{snapshot.SUITE_TIMEOUT_SECONDS}s', args[-1])
            self.assertIn('run_go_test_shards.py', args[-1])
            self.assertNotIn('-v', args)
            self.assertEqual(run.call_args_list[1].args[0][:3], ['docker', 'rm', '-f'])

    def test_healthy_budget_allows_start(self):
        result = self.run_guard(30 * 1048576)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('MUTATION_ALLOWED', result.stdout)


if __name__ == '__main__':
    unittest.main()
