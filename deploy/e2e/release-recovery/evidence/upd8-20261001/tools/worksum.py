#!/usr/bin/env python3
"""upd8 read-only: per-workload verdict and windows of a cell; journal timing of the record."""
import glob, json, sys, datetime as dt
lab = sys.argv[1]
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/"))[-1]
f = sorted(glob.glob(ev + "steps/*-verdicts/step.json"))[-1]
d = json.load(open(f))
def t(x):
    return dt.datetime.fromtimestamp(x, dt.timezone.utc).strftime("%H:%M:%S") if isinstance(x, (int, float)) else x
for k, v in (d["checks"].get("workloads") or {}).items():
    w = [(t(x.get("from")), t(x.get("to")), x.get("cause"), x.get("lower_bound_s"), x.get("upper_bound_s")) for x in (v.get("windows") or [])]
    print(k, v.get("verdict"), w, {kk: vv for kk, vv in v.items() if kk not in ("windows", "verdict", "note")})
print("host_panel", [(t(x.get("from")), t(x.get("to")), x.get("lower_bound_s"), x.get("upper_bound_s")) for x in d["checks"].get("host_panel_windows") or []])
print("judge", json.dumps({k: d["checks"].get(k) for k in ("card", "screen", "judge", "card_judge")}, ensure_ascii=False)[:1500])
