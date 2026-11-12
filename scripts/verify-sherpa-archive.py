"""Verify a downloaded/offline Sherpa executable archive before extraction."""
import hashlib
import json
from pathlib import Path
import re
import sys


def verify(archive, name, manifest):
    expected = manifest.get('archives', {}).get(name, '')
    if not re.fullmatch(r'[0-9a-f]{64}', expected):
        raise ValueError('Sherpa archive has no reviewed SHA-256: ' + name)
    digest = hashlib.sha256()
    with Path(archive).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    if digest.hexdigest() != expected:
        raise ValueError('Sherpa archive SHA-256 mismatch: ' + name)


if __name__ == '__main__':
    try:
        if len(sys.argv) != 4:
            raise ValueError('Usage: verify-sherpa-archive.py ARCHIVE NAME MANIFEST')
        verify(sys.argv[1], sys.argv[2], json.loads(Path(sys.argv[3]).read_text()))
    except (OSError, ValueError) as error:
        sys.exit(str(error))
