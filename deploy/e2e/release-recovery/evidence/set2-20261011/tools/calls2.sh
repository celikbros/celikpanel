#!/bin/bash
# usage: calls.sh LAB NODE SECTION-DIR-GLOB -> one line per call of the section (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/${DBG:-*}/ | tail -1)
python3 - "$ev" "$3" <<'PY'
import json, sys, glob, os
for path in sorted(glob.glob(os.path.join(sys.argv[1], "steps", sys.argv[2], "section.json"))):
    d = json.load(open(path))
    for c in d["calls"]:
        a = c.get("answer") or {}
        print(" ", c.get("status"), str(c.get("seconds", ""))[:6], c["label"][:78], "|", a.get("code"), a.get("reason"), "|", (a.get("error") or "")[:110], "|", json.dumps(a.get("vars"))[:160] if a.get("vars") else "", "|", c.get("replayed") or "", c.get("outcome") if c.get("outcome") not in (None, "answer") else "")
PY
