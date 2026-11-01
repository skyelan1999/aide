# 1x / 10x task time and token benchmark

Date: 2026-11-01

Model: DeepSeek Flash

Input: synthetic only; paired inputs are identical within each task type.

Usage source: DeepSeek upstream-reported usage, including preflight context compaction. No monetary cost is estimated.

| Task | Parallelism | Wall time | Total tokens | Prompt / completion | Workers | Coverage |
|---|---:|---:|---:|---:|---:|---|
| Simple extraction | 1x | 5.89s | 7,622 | 6,951 / 671 | 0 | pass |
| Simple extraction | 10x | 4.25s | 9,164 | 8,825 / 339 | 0 | pass |
| Ten-domain acceptance draft | 1x | 10.16s | 8,153 | 6,879 / 1,274 | 0 | **missed/duplicated labels** |
| Ten-domain acceptance draft | 10x | 24.43s | 45,454 | 36,143 / 9,311 | 10 | pass |

## Difference

- **Simple:** 10x took 1.64s less in this run, but used 1,542 more tokens (20.2%). Neither setting created worker tasks, so there was no parallel work to explain the small timing difference; treat it as provider variance. For simple work, 1x is more token-efficient.
- **Complex:** 10x took 2.40x as long and used 5.58x the tokens (+457.5%). It completed ten workers and covered all ten domains. 1x returned faster and cheaper, but did not pass the coverage check, so the outputs are not equivalent in completeness.
- The recorded totals include compression/preflight calls; the separate `taskOnlyTokensExcludingPreflight` field isolates normal task calls. For complex work this was 3,560 tokens at 1x and 40,888 at 10x.

## Limits

This is one live sample per setting, not a benchmark distribution. Wall time is affected by provider/network variation. The benchmark uses synthetic records and no real project data. The test command returned nonzero because the 1x complex output failed the exact-once label coverage assertion; that failure is retained as a result, not treated as a successful quality pass.

Raw synthetic proof: [parallelism-cost-benchmark.json](parallelism-cost-benchmark.json).
