#!/bin/bash
# usage: peek.sh LAB NODE [SECTION-GLOB]  -> section summaries of a running or finished cell (read-only)
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/$2/upd1/*/ 2>/dev/null | tail -1)
[ -n "$ev" ] || { echo "no evidence yet"; exit 0; }
ls $ev/steps 2>/dev/null | tr '\n' ' '; echo
python3 - "$ev" "${3:-}" <<'PY'
import json, sys, glob, os
ev, want = sys.argv[1], sys.argv[2]
for path in sorted(glob.glob(os.path.join(ev, "steps", "*", "section.json"))):
    if want and want not in path:
        continue
    d = json.load(open(path))
    print("==", d["section"], d["verdict"], d["started_at"], d["finished_at"], "substeps", d.get("substeps"))
    if d.get("error"):
        print("   ERROR", d["error"][:900])
    for c in d["checks"]:
        if c["ok"] is not True:
            print("   ", "FAIL" if c["ok"] is False else "UNKNOWN", c["name"][:160], "|", json.dumps(c["detail"])[:700])
    for n in d.get("notes", []):
        print("    note:", n["note"][:300])
for path in sorted(glob.glob(os.path.join(ev, "steps", "*", "step.json"))):
    d = json.load(open(path))
    if d["verdict"] not in ("passed",):
        print("step", d["name"], d["verdict"], (d.get("reason") or "")[:500])
PY
