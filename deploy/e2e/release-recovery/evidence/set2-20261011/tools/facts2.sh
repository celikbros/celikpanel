#!/bin/bash
# usage: facts.sh LAB NODE SECTION-DIR-GLOB [MAXCHARS] -> the section's own facts, checks that did not pass, answers (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/${DBG:-*}/ | tail -1)
python3 - "$ev" "$3" "${4:-3000}" <<'PY'
import json, sys, glob, os
STANDARD = {"section", "title", "node", "native_evidence", "started_at", "finished_at", "calls", "checks", "natives", "notes", "owner_actions", "verdict", "substeps"}
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", sys.argv[2], "section.json"))):
    d = json.load(open(path))
    print("==", d["section"], d["verdict"], d.get("error", "")[:1500])
    for c in d["checks"]:
        if c["ok"] is not True:
            print("  NOT-PASSED", c["ok"], c["name"][:200], "|", json.dumps(c["detail"])[:int(sys.argv[3])])
    print("  checks:", len(d["checks"]), "passed:", sum(1 for c in d["checks"] if c["ok"] is True))
    for n in d.get("notes", []):
        print("  NOTE", n["note"][:300])
PY
