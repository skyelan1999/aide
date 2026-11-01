#!/usr/bin/env python3
"""Run every registered server test in bounded, isolated test-binary processes."""
import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import os
import json
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


def run_shards(command, names, cwd, count=24, timeout=1000, workers=1, report_dir=None):
    if workers < 1:
        raise ValueError("workers must be positive")
    # Smaller processes release race shadow memory between groups within 4 GiB.
    groups = partition_tests(names, count)
    env = dict(os.environ, PWD=str(Path(cwd).resolve()))
    if report_dir is not None:
        report_dir = Path(report_dir)
        report_dir.mkdir(parents=True, exist_ok=True)

    def run(index, group):
        pattern = '^(' + '|'.join(re.escape(name) for name in group) + ')$'
        started = time.monotonic()
        # The compiled binary runs from its original Go package directory.
        # Preserve go test's unexpected os.Exit(0) protection and seed fuzz cases.
        args = command + ['-test.run=' + pattern, '-test.count=1',
                          '-test.paniconexit0', '-test.timeout=16m']
        if report_dir is not None:
            args.append('-test.v')
        with tempfile.TemporaryFile() as output:
            try:
                result = subprocess.run(args, cwd=cwd, env=env, stdout=output,
                                        stderr=subprocess.STDOUT, timeout=timeout)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
            output.seek(0)
            log = output.read().decode('utf-8', errors='replace')
        statuses = dict((name, status.lower()) for status, name in
                        re.findall(r'^--- (PASS|SKIP|FAIL): (\S+)', log, re.M))
        missing = [name for name in group if name not in statuses]
        if report_dir is not None:
            (report_dir / f'shard-{index:02d}.log').write_text(log)
            if missing and code == 0:
                code = 1
                log += '\nMissing test completion evidence: ' + ', '.join(missing)
        return index, len(group), code, time.monotonic() - started, log, group, statuses, missing

    print(f'Server inventory: {len(names)} top-level tests/fuzz seeds/examples; '
          f'{len(groups)} disjoint shards; {workers} worker(s); none omitted', flush=True)
    failed = []
    receipts = []
    with ThreadPoolExecutor(max_workers=min(workers, len(groups))) as pool:
        futures = [pool.submit(run, i + 1, group) for i, group in enumerate(groups)]
        for future in as_completed(futures):
            index, size, code, elapsed, log, selected, statuses, missing = future.result()
            receipts.append({'shard': index, 'selected': selected, 'exit': code,
                             'seconds': round(elapsed, 3), 'results': statuses, 'missing': missing})
            print(f'Server shard {index}/{len(groups)}: {size} selected, '
                  f'exit={code}, elapsed={elapsed:.1f}s', flush=True)
            if code:
                failed.append(index)
                print(log, flush=True)
    if report_dir is not None:
        report = {'status': 'fail' if failed else 'pass', 'inventory': names,
                  'inventoryCount': len(names), 'shards': sorted(receipts, key=lambda r: r['shard']),
                  'passed': sorted(n for r in receipts for n, s in r['results'].items() if s == 'pass'),
                  'skipped': sorted(n for r in receipts for n, s in r['results'].items() if s == 'skip'),
                  'failedShards': sorted(failed), 'notice': 'Skipped is not executed functionality; inspect per-shard logs for reasons.'}
        (report_dir / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    if failed:
        raise RuntimeError('Server test shards failed: ' + ', '.join(map(str, sorted(failed))))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', type=Path)
    parser.add_argument('--cwd', type=Path, required=True)
    parser.add_argument('--workers', type=int, choices=[1, 2], default=1)
    parser.add_argument('--report-dir', type=Path)
    args = parser.parse_args()
    command = [str(args.binary.resolve())]
    result = subprocess.run(command + ['-test.list=.'], cwd=args.cwd,
                            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            check=True, timeout=30)
    run_shards(command, test_names(result.stdout), args.cwd, workers=args.workers, report_dir=args.report_dir)


if __name__ == '__main__':
    main()
