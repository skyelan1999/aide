#!/usr/bin/env python3
"""Fast push/PR gate: compile all packages, critical regressions, then vet.

No Docker image builds, network probes, paid models or exhaustive test runs.
The comprehensive native/race/browser/plugin suites belong to Aide Delivery.
"""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
PATTERN = (r'^Test(AuthenticationAndOrigin|FileBoundariesAndConflicts|SettingsNeverReturnKey|'
           r'ExecutionDiffLargeArtifactsUseBoundedMemory|ExecutionTextArtifactMatchesProposalTextContract|'
           r'ExecutionCollectPartialFailureRetainsIdempotentReceipts|StaticAuditFixture|'
           r'ExecutionScheduler.*|Incidents.*|ContextRecoverySummaryExtractsWrappedJSON|'
           r'ParallelReadOnlyToolBatchEligibility|ShellPathGuard.*)$')
COMMANDS = [
    ['go', 'test', '-mod=vendor', '-count=1', '-timeout=5m', '-run=' + PATTERN, './...'],
    ['go', 'vet', '-mod=vendor', './...'],
]


def main():
    for command in COMMANDS:
        print('BASIC:', ' '.join(command), flush=True)
        subprocess.run(command, cwd=ROOT, check=True, timeout=600)


if __name__ == '__main__':
    main()
