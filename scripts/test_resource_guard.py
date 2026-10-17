#!/usr/bin/env python3
"""Exercise launcher safety boundaries without starting a real Docker engine."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

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

    def test_healthy_budget_allows_start(self):
        result = self.run_guard(30 * 1048576)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('MUTATION_ALLOWED', result.stdout)


if __name__ == '__main__':
    unittest.main()
