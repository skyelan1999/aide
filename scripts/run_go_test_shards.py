#!/usr/bin/env python3
"""Run every registered server test in bounded, isolated test-binary processes."""
import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time


def test_names(output):
    names = [line for line in output.splitlines()
             if re.fullmatch(r'(?:Test|Fuzz|Example)\w*', line)]
    if not names or len(names) != len(set(names)):
        raise ValueError('Expected a nonempty, unique test-binary inventory')
    return names


def partition_tests(names, count):
    if count < 1 or not names or len(names) != len(set(names)):
        raise ValueError('Invalid shard count or test inventory')
    groups = [names[i::count] for i in range(min(count, len(names)))]
    assert sorted(name for group in groups for name in group) == sorted(names)
    return groups


def run_shards(command, names, cwd, count=24, timeout=1000, workers=1):
    if workers < 1:
        raise ValueError("workers must be positive")
    # Smaller processes release race shadow memory between groups within 4 GiB.
    groups = partition_tests(names, count)
    env = dict(os.environ, PWD=str(Path(cwd).resolve()))

    def run(index, group):
        pattern = '^(' + '|'.join(re.escape(name) for name in group) + ')$'
        started = time.monotonic()
        # The compiled binary runs from its original Go package directory.
        # Preserve go test's unexpected os.Exit(0) protection and seed fuzz cases.
        args = command + ['-test.run=' + pattern, '-test.count=1',
                          '-test.paniconexit0', '-test.timeout=16m']
        with tempfile.TemporaryFile() as output:
            try:
                result = subprocess.run(args, cwd=cwd, env=env, stdout=output,
                                        stderr=subprocess.STDOUT, timeout=timeout)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
            output.seek(0)
            log = output.read().decode('utf-8', errors='replace')
        return index, len(group), code, time.monotonic() - started, log

    print(f'Server inventory: {len(names)} top-level tests/fuzz seeds/examples; '
          f'{len(groups)} disjoint shards; {workers} worker(s); none omitted', flush=True)
    failed = []
    with ThreadPoolExecutor(max_workers=min(workers, len(groups))) as pool:
        futures = [pool.submit(run, i + 1, group) for i, group in enumerate(groups)]
        for future in as_completed(futures):
            index, size, code, elapsed, log = future.result()
            print(f'Server shard {index}/{len(groups)}: {size} selected, '
                  f'exit={code}, elapsed={elapsed:.1f}s', flush=True)
            if code:
                failed.append(index)
                print(log, flush=True)
    if failed:
        raise RuntimeError('Server test shards failed: ' + ', '.join(map(str, sorted(failed))))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', type=Path)
    parser.add_argument('--cwd', type=Path, required=True)
    args = parser.parse_args()
    command = [str(args.binary.resolve())]
    result = subprocess.run(command + ['-test.list=.'], cwd=args.cwd,
                            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            check=True, timeout=30)
    run_shards(command, test_names(result.stdout), args.cwd)


if __name__ == '__main__':
    main()
