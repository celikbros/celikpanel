#!/usr/bin/env python3
"""upd13 host-only, read-only: gaps in the guest sampler (guest-samples.jsonl) and the host sampler
(host-samples.jsonl) of every staged run; any gap over 12 s is listed. usage: gaps13.py STAGE OUT"""
import datetime as dt, json, sys
from pathlib import Path
stage, out = Path(sys.argv[1]), Path(sys.argv[2])
L = ["# sampler gaps over 12 s per run (5 s samplers; reboot/reset windows of the cell appear here too)"]
for run in sorted(stage.glob("upd1-*/run-*")) + sorted(stage.glob("part2-alpha80/upd1-*/run-*")):
    for name in ("guest-samples.jsonl", "host-samples.jsonl"):
        for f in sorted(run.glob(f"steps/*-collect/{name}")):
            ts = []
            for line in open(f, encoding="utf-8"):
                if line.strip():
                    u = json.loads(line).get("utc")
                    if isinstance(u, (int, float)):
                        ts.append(dt.datetime.fromtimestamp(u, dt.timezone.utc))
                    elif u:
                        ts.append(dt.datetime.fromisoformat(str(u).replace("Z", "+00:00")))
            ts.sort()
            gaps = [(a, b, (b - a).total_seconds()) for a, b in zip(ts, ts[1:]) if (b - a).total_seconds() > 12]
            L.append(f"{run.relative_to(stage)} {name}: samples={len(ts)} max_gap={max([(b - a).total_seconds() for a, b in zip(ts, ts[1:])] or [0]):.1f}s gaps>12s={len(gaps)}")
            for a, b, s in gaps:
                L.append(f"    {a:%H:%M:%S}-{b:%H:%M:%S} {s:.1f}s")
out.write_text("\n".join(L) + "\n", encoding="utf-8")
print("\n".join(L))
