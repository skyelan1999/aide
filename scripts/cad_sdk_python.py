"""SDK launcher installed beside step-engine/ and readers/ as cad_python.py.

Resolve sibling SDK utilities as well as project sources and pinned CAD
dependencies. Forward normal Python flags and arguments without a shell.
This launcher does not install dependencies or construct any geometry.
"""
import os
import sys
from pathlib import Path


def cad_environment(base, previous):
    base = Path(base).resolve()
    engine = base / 'step-engine'
    env = dict(previous)
    paths = {
        'PYTHONPATH': [base, base.parent / '03_\u4e09\u7ef4\u5efa\u6a21', engine, base / 'readers'],
        'LD_LIBRARY_PATH': [engine / 'system'],
    }
    for key, directories in paths.items():
        parts = [str(path) for path in directories]
        if env.get(key):
            parts.append(env[key])
        env[key] = os.pathsep.join(parts)
    return env


def main():
    base = Path(__file__).resolve().parent
    if not (base / 'step-engine' / 'OCP').is_dir():
        raise SystemExit('Project CAD engine is missing: ' + str(base / 'step-engine'))
    if len(sys.argv) < 2:
        raise SystemExit('Usage: python3 cad_python.py SCRIPT.py [args] (or -c CODE)')
    os.execve(sys.executable, [sys.executable, *sys.argv[1:]], cad_environment(base, os.environ))


if __name__ == '__main__':
    main()
