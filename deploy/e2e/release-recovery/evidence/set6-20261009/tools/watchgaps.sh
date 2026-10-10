#!/bin/bash
# set6 read-only: did the host stop executing? (1) gaps between the 30 s readings of the Windows C: watcher
# (c-drive-watch.txt, written by cwatch.ps1 on Windows); (2) per cell, the gap between the end of each step and the
# start of the next, and the longest step; (3) the readings of the watcher inside each cell's wrapper window.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set6-run
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/$(cat $R/evidence-name.txt)"
python3 -I - "$J/c-drive-watch.txt" "$E" <<'PY'
import calendar, glob, json, os, sys, time
def epoch(stamp):
    return calendar.timegm(time.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ"))
stamps = [line.split()[0] for line in open(sys.argv[1], encoding="utf-8", errors="replace") if line[:2] == "20"]
times = [epoch(s) for s in stamps]
gaps = [(stamps[i - 1], stamps[i], times[i] - times[i - 1]) for i in range(1, len(times)) if times[i] - times[i - 1] > 45]
print(f"Windows C: watcher: {len(stamps)} readings from {stamps[0]} to {stamps[-1]}; largest gap {max([times[i] - times[i-1] for i in range(1, len(times))] or [0])} s; gaps over 45 s: {len(gaps)}")
for a, b, g in gaps:
    print(f"  GAP {g} s between {a} and {b}")
for result in sorted(glob.glob(os.path.join(sys.argv[2], "*", "*", "run-*", "result.json"))):
    run = os.path.dirname(result)
    steps = json.load(open(result, encoding="utf-8")).get("steps") or []
    between = [(steps[i - 1]["name"], steps[i]["name"], epoch(steps[i]["started_at"]) - epoch(steps[i - 1]["finished_at"]))
               for i in range(1, len(steps)) if steps[i].get("started_at") and steps[i - 1].get("finished_at")]
    longest = max(((s["name"], epoch(s["finished_at"]) - epoch(s["started_at"])) for s in steps if s.get("started_at") and s.get("finished_at")),
                  key=lambda item: item[1], default=("-", 0))
    try:
        start = epoch(open(os.path.join(run, "host", "wrapper.start.txt")).read().strip())
        end = epoch(open(os.path.join(run, "host", "wrapper.end.txt")).read().strip())
    except (OSError, ValueError):
        start = end = 0
    inside = [t for t in times if start <= t <= end]
    inside_gap = max([inside[i] - inside[i - 1] for i in range(1, len(inside))] or [0])
    in_gaps = [g for g in gaps if epoch(g[0]) < end and epoch(g[1]) > start]
    print(f"{os.path.relpath(run, sys.argv[2])}: {len(steps)} steps; largest pause between two steps {max([b[2] for b in between] or [0])} s; "
          f"longest step {longest[0]} {longest[1]} s; watcher readings inside the wrapper window: {len(inside)}, largest gap between them {inside_gap} s; "
          f"watcher gaps over 45 s that touch the window: {len(in_gaps)}")
PY
