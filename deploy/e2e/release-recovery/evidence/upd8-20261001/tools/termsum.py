#!/usr/bin/env python3
"""upd8 read-only: terminal and verdict checks of a cell (keys and compact values)."""
import glob, json, sys
lab = sys.argv[1]
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/"))[-1]
for name in ("terminal", "verdicts", "kind-expectation", "track", "owner-continuation"):
    for f in sorted(glob.glob(ev + f"steps/*-{name}*/step.json")):
        d = json.load(open(f))
        print("=====", f.split("/steps/")[1], d.get("verdict"))
        for k, v in (d.get("checks") or {}).items():
            print(" ", k, "=", json.dumps(v, ensure_ascii=False)[: int(sys.argv[2]) if len(sys.argv) > 2 else 400])
r = json.load(open(ev + "result.json"))
print("===== result keys", list(r))
for k in ("workloads", "continuity", "scope", "outcome", "panel_down", "verdicts"):
    if k in r:
        print(k, "=", json.dumps(r[k], ensure_ascii=False)[:2500])
