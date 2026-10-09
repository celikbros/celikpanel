"""set3: host pauses seen from inside the cells. For every staged run: the largest gap between consecutive records of
its own evidence clock (the wrapper's step times; for update cells also the guest sampler's samples), so that a host
standby inside a cell would show. usage: gaps.py EVIDENCE_DIR  -> writes sampler-gaps.txt
"""
import datetime as dt
import glob
import json
import os
import sys

E = sys.argv[1].rstrip("/\\")
lines = ["run | steps | largest gap between a step's end and the next step's start (s) | host sampler samples | largest gap between two host samples (s), starting at (UTC)"]


def stamp(text):
    try:
        return dt.datetime.strptime(text, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=dt.timezone.utc).timestamp()
    except (TypeError, ValueError):
        return None


for result in sorted(glob.glob(os.path.join(E, "**", "result.json"), recursive=True)):
    run = os.path.dirname(result)
    rel = os.path.relpath(run, E).replace("\\", "/")
    try:
        steps = json.load(open(result, encoding="utf-8")).get("steps", [])
    except (OSError, ValueError):
        continue
    gap = 0.0
    for a, b in zip(steps, steps[1:]):
        x, y = stamp(a.get("finished_at")), stamp(b.get("started_at"))
        if x is not None and y is not None:
            gap = max(gap, y - x)
    times = []
    for path in glob.glob(os.path.join(run, "steps", "*", "host-samples.jsonl")):
        for line in open(path, encoding="utf-8"):
            try:
                sample = json.loads(line)
            except ValueError:
                continue
            if isinstance(sample, dict) and isinstance(sample.get("t"), (int, float)):
                times.append(float(sample["t"]))
    times = sorted(set(times))
    largest, at = 0.0, None
    for a, b in zip(times, times[1:]):
        if b - a > largest:
            largest, at = b - a, a
    when = dt.datetime.fromtimestamp(at, dt.timezone.utc).strftime("%H:%M:%S") if at else "-"
    lines.append(f"{rel} | {len(steps)} | {gap:.0f} | {len(times)} | {largest:.1f} at {when}")
open(os.path.join(E, "sampler-gaps.txt"), "w", encoding="utf-8", newline="\n").write("\n".join(lines) + "\n")
print("\n".join(lines[:40]))
