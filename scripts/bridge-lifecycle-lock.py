"""Serialize host bridge election through readiness and stop, with a kernel lock.

The persistent lock file is never removed; kernel ownership ends on process exit.
"""
import fcntl
import os
from pathlib import Path
import stat
import subprocess
import sys

if __name__ == '__main__':
    pid_file, script, *args = sys.argv[1:]
    Path(pid_file).parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    descriptor = os.open(pid_file + '.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            sys.exit('Bridge lock must be a regular file')
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        result = subprocess.run(['bash', script, *args], env={**os.environ, 'AIDE_BRIDGE_LOCKED': pid_file})
        sys.exit(result.returncode)
    finally:
        os.close(descriptor)
