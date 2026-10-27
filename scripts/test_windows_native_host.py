#!/usr/bin/env python3
"""Run the production native host transaction core on the existing host compiler.

This does not establish Windows process, ACL, service or Docker Desktop behavior.
"""
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    compiler = shutil.which('c++')
    if not compiler:
        raise SystemExit('NOT_RUN: an existing C++ compiler is required')
    with tempfile.TemporaryDirectory(prefix='aide-native-host-') as directory:
        executable = Path(directory) / 'host-core'
        subprocess.run([compiler, '-std=c++17', '-O2', str(ROOT / 'scripts/native_host_portable.cpp'),
                        '-o', str(executable)], check=True)
        subprocess.run([str(executable)], check=True, timeout=30)


if __name__ == '__main__':
    main()
