#!/usr/bin/env python3
"""usage: gsamples.py CELL RUN FROM_EPOCH TO_EPOCH -> guest samples in the window (panel/web/cron/db fields)."""
import glob, json, sys
S = "/var/tmp/cp-upd4-run/stage/upd4-20261001"
cell, run, a, b = sys.argv[1], sys.argv[2], float(sys.argv[3]), float(sys.argv[4])
f = sorted(glob.glob(f"{S}/{cell}/{run}/steps/*-collect/guest-samples.jsonl"))[0]
for line in open(f):
    if not line.strip():
        continue
    s = json.loads(line)
    if a <= float(s.get("t", 0)) <= b:
        print(s.get("utc"), round(float(s["t"]), 3), "boot", str(s.get("boot_id"))[:8], "panel", json.dumps(s.get("panel"))[:200],
              "web", json.dumps((s.get("web") or {}).get("ok")), "db", json.dumps(s.get("db"))[:120])
