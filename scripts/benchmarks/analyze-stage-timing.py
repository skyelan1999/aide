#!/usr/bin/env python3
"""Analyze payload-free benchmark timestamps; overlap is never summed twice."""
import argparse
import calendar
import datetime
import json
from pathlib import Path
import re


PRIORITY = {
    "context_compaction": 100,
    "verification": 90,
    "read_and_persist": 80,
    "proposals_and_persist": 80,
    "schedule": 70,
    "coordination": 70,
    "collect_and_wait": 60,
    "queue_execution": 65,
    "other_tool": 50,
    "model_request": 40,
    "compaction_or_auxiliary_api": 35,
}


def iso_ns(value):
    fractional = re.search(r"\.(\d+)", value)
    ns = int((fractional.group(1) + "0" * 9)[:9]) if fractional else 0
    whole = re.sub(r"\.\d+", "", value).replace("Z", "+00:00")
    date = datetime.datetime.fromisoformat(whole)
    return calendar.timegm(date.utctimetuple()) * 1_000_000_000 + ns


def exclusive_wall(spans, start, end):
    selected = []
    for span in spans:
        if span["Actor"] != "main":
            continue
        low, high = max(start, span["StartNS"]), min(end, span["EndNS"])
        if high > low:
            selected.append((low, high, span["Phase"]))
    boundaries = sorted({start, end} | {x for lo, hi, _ in selected for x in (lo, hi)})
    totals = {}
    for lo, hi in zip(boundaries, boundaries[1:]):
        active = [phase for a, b, phase in selected if a <= lo and hi <= b]
        phase = max(active, key=lambda p: PRIORITY.get(p, 0)) if active else "runtime_or_unattributed"
        totals[phase] = totals.get(phase, 0) + hi - lo
    assert sum(totals.values()) == end - start
    return {phase: duration / 1_000_000_000 for phase, duration in totals.items()}


def union_duration(spans):
    merged, total = None, 0
    for low, high in sorted(spans):
        if high <= low:
            continue
        if merged is None:
            merged = [low, high]
        elif low > merged[1]:
            total += merged[1] - merged[0]
            merged = [low, high]
        else:
            merged[1] = max(merged[1], high)
    if merged:
        total += merged[1] - merged[0]
    return total / 1_000_000_000


def analyze(trace, proof):
    result = []
    for case in proof:
        start = iso_ns(case["startedAt"])
        end = start + int(case["elapsedMillis"]) * 1_000_000
        total = (end - start) / 1_000_000_000
        main = exclusive_wall(trace["spans"], start, end)
        workers = {}
        for iteration in case.get("iterations", []):
            for worker in (iteration.get("environments") or {}).get("results", []):
                workers[worker["id"]] = worker
        lifetimes = []
        durations = []
        for worker in workers.values():
            if not worker.get("startedAt") or not worker.get("finishedAt"):
                continue
            low, high = iso_ns(worker["startedAt"]), iso_ns(worker["finishedAt"])
            lifetimes.append((low, high))
            durations.append({"id": worker["id"], "status": worker["status"],
                              "seconds": (high - low) / 1_000_000_000,
                              "queueSeconds": (low - iso_ns(worker["createdAt"])) / 1_000_000_000})
        row = {
            "name": case["name"], "condition": trace["condition"],
            "verifiedComplete": case.get("verifiedComplete") is True,
            "totalSeconds": total, "exclusiveMainSeconds": main,
            "exclusiveMainPercent": {p: 100 * seconds / total for p, seconds in main.items()},
            "workerLifetimes": durations,
            "workerActiveUnionSeconds": union_duration(lifetimes),
            "cumulativeWorkerSecondsNotAdditiveToWall": sum(x["seconds"] for x in durations),
            "unpairedEvents": trace["unpairedEvents"], "openIntervals": trace["openIntervals"],
            "measurement": "Main wall-clock partition sums to total. Worker lifetimes overlap it. Model HTTP includes network, upstream queue and inference; not pure reasoning time. Read/tool intervals include persistence. Unknown time is explicitly unassigned.",
        }
        if lifetimes:
            created = min(iso_ns(w["createdAt"]) for w in workers.values())
            finished = max(high for _, high in lifetimes)
            row["chronologicalMilestonesSeconds"] = {
                "beforeFirstWorker": (created - start) / 1_000_000_000,
                "firstWorkerToLastFinish": (finished - created) / 1_000_000_000,
                "afterWorkers": (end - finished) / 1_000_000_000,
            }
        result.append(row)
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("trace", type=Path)
    parser.add_argument("--proof", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    proof = args.proof or args.trace.with_name(args.trace.stem + "-proof.json")
    result = analyze(json.loads(args.trace.read_text()), json.loads(proof.read_text()))
    text = json.dumps(result, ensure_ascii=False, indent=2) + "\n"
    if args.output:
        args.output.write_text(text)
    print(text)


if __name__ == "__main__":
    main()
