#!/usr/bin/env python3
"""usage: apis.py CELL STEPGLOB [RUN] -> status and a body excerpt of every API exchange in the matching step dirs."""
import glob, json, sys
S = "/var/tmp/cp-upd4-run/stage/upd4-20261001"
cell, step = sys.argv[1], sys.argv[2]
run = sys.argv[3] if len(sys.argv) > 3 else "run-a"
for f in sorted(glob.glob(f"{S}/{cell}/{run}/steps/*{step}*/api/*.json")):
    d = json.load(open(f))
    name = f.split("/steps/")[-1]
    resp = d.get("response") or {}
    req = d.get("request") or {}
    body = resp.get("body") if "body" in resp else resp.get("json", resp.get("text"))
    print(name, "|", req.get("method"), req.get("path") or req.get("url"), "| status", resp.get("status"), "| at", d.get("at") or d.get("utc"))
    print("   ", json.dumps(body, ensure_ascii=False)[:500])
    if not resp:
        print("    keys", sorted(d)[:20], json.dumps(d)[:400])
