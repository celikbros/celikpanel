#!/usr/bin/env python3
"""Read-only live view of a running cell's status samples (host only). usage: live.py LAB [N]"""
import glob, json, sys
lab = sys.argv[1]; n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/*/upd1/*/"))[-1]
files = sorted(glob.glob(ev + "steps/*/samples/*.json"))
print(ev, len(files), "samples")
for f in files[-n:]:
    try:
        s = json.load(open(f))
    except Exception as e:
        print(f, "unreadable", e); continue
    r = (s.get("recovery_api") or {}).get("body") or {}
    try:
        cli = json.loads(((s.get("cli") or {}).get("json") or {}).get("stdout") or "null") or {}
    except Exception:
        cli = {}
    print(s.get("utc", "")[11:19], f.split("/")[-3][:10], "R", r.get("phase"), r.get("automatic_recovery"), "| CLI", cli.get("phase"), cli.get("reason"),
          cli.get("terminal_proof"), cli.get("automatic_recovery"), "fc=", cli.get("failure_code"), "ffc=", cli.get("first_failure_code"),
          "ren=", cli.get("renewal_before_update"), "|", (s.get("agreement") or {}).get("verdict"))
steps = sorted(glob.glob(ev + "steps/*/"))
print("steps:", [x.split("/")[-2] for x in steps])
