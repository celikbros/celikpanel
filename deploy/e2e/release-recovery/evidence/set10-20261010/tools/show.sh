#!/bin/bash
# set10: print one section of a lab's evidence: run errors and the checks not held. usage: show.sh LAB STEP-SUBSTRING [all]
f=$(find /var/tmp/cp-release-drill-$1/evidence -path "*steps/*$2*/section.json" | sort | tail -1)
echo "$f"
python3 -I - "$f" "${3:-bad}" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
for r in s.get("runs") or []:
    if r.get("error"):
        print("RUN", r.get("run"), "ERROR", r["error"][:1500])
for name, c in (s.get("cells") or {}).items():
    print("CELL", name, c["verdict"])
for c in s["checks"]:
    if sys.argv[2] != "all" and c["ok"] is True:
        continue
    print(c["ok"], c["name"][:160])
    if c["ok"] is not True:
        print("    ", json.dumps(c["detail"], default=str)[:1200])
print("error:", (s.get("error") or "")[:1500])
PY
