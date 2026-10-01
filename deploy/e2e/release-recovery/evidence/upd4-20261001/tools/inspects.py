#!/usr/bin/env python3
"""usage: inspects.py CELL [RUN] [NAMEFILTER] -> list inspection files and print the panel unit and timers of each."""
import glob, json, sys
S = "/var/tmp/cp-upd4-run/stage/upd4-20261001"
cell = sys.argv[1]
run = sys.argv[2] if len(sys.argv) > 2 else "run-a"
flt = sys.argv[3] if len(sys.argv) > 3 else ""
files = sorted(glob.glob(f"{S}/{cell}/{run}/**/inspect-*.json", recursive=True))
print(len(files), "inspection files")
for f in files:
    name = f.split(run + "/")[-1]
    if flt and flt not in name:
        print(" ", name)
        continue
    d = json.load(open(f))
    print("==", name, "keys", sorted(d)[:25])
    txt = json.dumps(d)
    for key in ("NRestarts", "Result", "ActiveState"):
        pass
    def find(o, path=""):
        if isinstance(o, dict):
            for k, v in o.items():
                if k in ("celikpanel-panel.service", "certbot.timer", "certbot-renew.timer", "units", "timers", "point", "at", "utc"):
                    print("   ", path + "/" + k, json.dumps(v)[:600])
                else:
                    find(v, path + "/" + k)
    find(d)
