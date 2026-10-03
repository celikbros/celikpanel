#!/usr/bin/env python3
"""upd13 host-only, read-only: one markdown row per staged run (wrapper times, lab, harness copy, outcome, attempts,
key times from the root CLI track samples). usage: cells13.py STAGE OUT"""
import json
import re
import sys
from pathlib import Path

stage, out = Path(sys.argv[1]), Path(sys.argv[2])
rows = ["| Cell (folder) | Lab | Harness | Wrapper (UTC) | Owner start | Outcome | Attempts (automatic / owner) | Kind | Overall |",
        "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"]
runs = sorted(stage.glob("upd1-*/run-*")) + sorted(stage.glob("part2-alpha80/upd1-*/run-*"))
for run in runs:
    res = json.load(open(run / "result.json"))
    host = run / "host"
    cell = str(run.relative_to(stage))
    start = (host / "wrapper.start.txt").read_text().strip()
    end = (host / "wrapper.end.txt").read_text().strip()
    lab = (host / "lab.txt").read_text().splitlines()[0].split("cp-release-drill-")[-1]
    port = re.search(r" (\d+)\s*$", (host / "job.sh").read_text().strip().splitlines()[-1])
    harness = "h22" if "harness-h22" in (host / "harness.txt").read_text() else "base"
    steps = {s["name"]: s for s in res["steps"]}
    o = res.get("outcome") or {}
    att = o.get("attempts") or {}
    auto = ", ".join(f"{a.get('operation')}/{a.get('phase')} {str(a.get('at'))[11:19]}" for a in att.get("automatic") or [])
    owner = ", ".join(f"{a.get('operation')}/{a.get('phase')} {str(a.get('at'))[11:19]}" for a in att.get("owner") or [])
    kind = ((res.get("kind") or {}).get("judged") or {}).get("verdict") or "-"
    fault = ""
    if o.get("reboot"):
        fault = " + QMP reset"
    os_ = (steps.get("owner-start") or {}).get("started_at", "")[11:19]
    rows.append(f"| {cell} | {lab}, {port[1] if port else '?'} | {harness} | {start[11:19]}-{end[11:19]} | {os_ or '-'} | "
                f"`{o.get('classification')}`{fault} | {att.get('automatic_count', 0)}: {auto or '-'} / {att.get('owner_count', 0)}"
                f"{': ' + owner if owner else ''} | {kind} | {res.get('overall')} |")
out.write_text("\n".join(rows) + "\n", encoding="utf-8")
print("\n".join(rows))
