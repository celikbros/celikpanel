#!/usr/bin/env python3
"""usage: mgmtsteps.py CELL RUN -> the management-off steps' checks (services trimmed), plus panel truth files."""
import glob, json, sys
S = "/var/tmp/cp-upd4-run/stage/upd4-20261001"
cell, run = sys.argv[1], sys.argv[2]
base = f"{S}/{cell}/{run}"
for name in ("management-off", "owner-reboot", "management-off-measure", "management-return", "verdicts"):
    f = sorted(glob.glob(f"{base}/steps/*-{name}/step.json"))
    if not f:
        continue
    d = json.load(open(f[0]))
    c = d.get("checks") or {}
    for k in ("services",):
        c.pop(k, None)
    if name == "verdicts":
        c = {"workloads": {k: {"verdict": v.get("verdict"), "windows": v.get("windows")} for k, v in (c.get("workloads") or {}).items()},
             "samples_until_management_off": c.get("samples_until_management_off"), "agreement": c.get("agreement"),
             "host_panel_windows": c.get("host_panel_windows")}
    print(f"== {name}: {d.get('verdict')} {d.get('started_at')} -> {d.get('finished_at')} {d.get('reason') or ''}")
    print(json.dumps(c, ensure_ascii=False, sort_keys=True)[:6000])
for f in sorted(glob.glob(f"{base}/steps/*/panel-truth-*.json")):
    print("==", f.split(run + "/")[-1])
    print(open(f).read()[:1500])
