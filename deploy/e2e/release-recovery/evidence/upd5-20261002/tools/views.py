#!/usr/bin/env python3
"""Per cell: how many status samples each view answered (Panel API, root CLI) and the agreement verdicts."""
import glob, json, sys
S = "/var/tmp/cp-upd5-run/stage/upd5-20261002"
out = []
for run in sorted(glob.glob(S + "/upd1-*/run-*")):
    files = sorted(glob.glob(run + "/steps/*-track*/samples/*.json"))
    api = cli = both = 0
    verdicts = {}
    for f in files:
        s = json.load(open(f))
        a = (s.get("recovery_api") or {}).get("http") == 200
        try:
            c = bool(json.loads(((s.get("cli") or {}).get("json") or {}).get("stdout") or "null"))
        except Exception:
            c = False
        api += a; cli += c; both += a and c
        v = (s.get("agreement") or {}).get("verdict")
        verdicts[v] = verdicts.get(v, 0) + 1
    out.append(f"{run.split(S + '/')[1]:45s} samples={len(files):3d} api={api:3d} cli={cli:3d} both={both:3d} agreement={json.dumps(verdicts)}")
open(S + "/views-per-cell.txt", "w").write("\n".join(out) + "\n")
print("\n".join(out))
