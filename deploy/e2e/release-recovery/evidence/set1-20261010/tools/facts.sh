#!/bin/bash
# usage: facts.sh LAB NODE SECTION-DIR-GLOB  -> the section's own facts and answers (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 - "$ev" "$3" <<'PY'
import json, sys, glob, os
STANDARD = {"section", "title", "node", "native_evidence", "started_at", "finished_at", "calls", "checks", "natives", "notes", "owner_actions", "verdict", "substeps"}
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", sys.argv[2], "section.json"))):
    d = json.load(open(path))
    print(json.dumps({k: v for k, v in d.items() if k not in STANDARD}, indent=1)[:5000])
    for c in d["calls"]:
        a = c.get("answer") or {}
        print(" ", c["status"], c["label"][:60], "|", a.get("code"), a.get("reason"), "|", (a.get("error") or "")[:160], "|", json.dumps(a.get("vars"))[:300] if a.get("vars") else "", "| keys:", sorted((c.get("catalogue_texts") or {})))
PY
