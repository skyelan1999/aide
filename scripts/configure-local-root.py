#!/usr/bin/env python3
"""Configure the Docker-visible host directory without restarting services."""
import argparse
import os
from pathlib import Path


def configure(project, directory):
    directory = Path(directory).expanduser().resolve(strict=True)
    if not directory.is_dir():
        raise ValueError('Local root must be an existing directory')
    value = str(directory)
    if any(c in value for c in '\n\r\x00\''):
        raise ValueError('Unsupported characters in directory path')
    target = Path(project) / '.env'
    source = target if target.exists() else Path(project) / '.env.example'
    lines = source.read_text().splitlines()
    # Single quotes preserve spaces and literal $ characters in Compose dotenv.
    entry = "AIDE_LOCAL_ROOT='" + value + "'"
    replaced = False
    output = []
    for line in lines:
        if line.strip().startswith('AIDE_LOCAL_ROOT='):
            if not replaced:
                output.append(entry)
                replaced = True
        else:
            output.append(line)
    if not replaced:
        output.append(entry)
    temp = target.with_name('.env.local-root-tmp')
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, 'w') as stream:
            stream.write('\n'.join(output) + '\n')
        os.replace(temp, target)
    finally:
        temp.unlink(missing_ok=True)
    return directory


if __name__ == '__main__':
    project = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--path', default=str(project), help='Existing host directory; defaults to this aide repository')
    args = parser.parse_args()
    try:
        result = configure(project, args.path)
    except (OSError, ValueError) as error:
        parser.exit(1, str(error) + '\n')
    print(f'Local browsing root: {result}')
    print('Saved to .env. Recreate the aide container to apply the mount; no service was restarted.')
