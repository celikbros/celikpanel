#!/usr/bin/env python3
"""usage: sample.py CELL INDEX [RUN] -> prints selected parts of one status sample (host, read-only)."""
import glob, json, sys
S = "/var/tmp/cp-upd4-run/stage/upd4-20261001"
cell, idx = sys.argv[1], int(sys.argv[2])
run = sys.argv[3] if len(sys.argv) > 3 else "run-a"
files = sorted(glob.glob(f"{S}/{cell}/{run}/steps/*-track*/samples/*.json"))
print(len(files), "samples; showing", files[idx].split(run + "/")[-1])
s = json.load(open(files[idx]))
print("keys", sorted(s))
for k in ("update_status", "recovery_api"):
    print(k, json.dumps(s.get(k), ensure_ascii=False)[:1500])
for k in ("update_card", "guidance_api", "guidance_cli", "recovery_screen"):
    print(k, json.dumps(s.get(k), ensure_ascii=False)[:2500])
print("cli.json", ((s.get("cli") or {}).get("json") or {}).get("stdout", "")[:1500])
