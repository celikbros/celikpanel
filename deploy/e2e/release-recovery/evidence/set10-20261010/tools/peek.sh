#!/bin/bash
# set10: list the native files of one section and print selected facts. usage: peek.sh LAB STEP-SUBSTRING
d=$(find /var/tmp/cp-release-drill-$1/evidence -path "*steps/*$2*" -maxdepth 6 -type d -name "*$2*" | sort | tail -1)
ls $d/native | head -80
python3 -I - "$d" <<'PY'
import json, sys, glob, os
d = sys.argv[1]
for f in sorted(glob.glob(d + "/native/*journal*.json")):
    v = json.load(open(f))
    print("==", os.path.basename(f))
    for u, lines in v["by_unit"].items():
        for l in lines:
            if "site configuration" in l or "Reload" in l or "error" in l.lower():
                print("  ", u.split(".")[0], l[:260])
for f in sorted(glob.glob(d + "/native/*after-read.json"))[:2]:
    v = json.load(open(f))
    for dom, s in v["sites"].items():
        print("==", os.path.basename(f), dom, {k: s["vhost"].get(k) for k in ("sha256", "inode", "mode")},
              "pending", s["side_files"]["pending"].get("exists"), "backups", len(s["side_files"]["backups"]))
    print("ledger", json.dumps([{k: r.get(k) for k in ("domain_id", "state", "state_reason", "pending_path", "decision")} for r in v["ledger"].get("managed_site_files", [])]))
PY
