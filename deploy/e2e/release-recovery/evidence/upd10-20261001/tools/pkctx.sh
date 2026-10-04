#!/bin/bash
# usage: pkctx.sh LAB -> packagekit journal start/quit lines; distinct context processes and backend sets (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
ls $ev/steps/
grep -h -E "daemon (start|quit)" $ev/steps/*collect/journal-packagekit.txt 2>/dev/null | cut -c1-160
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
seen=set()
for f in sorted(glob.glob(ev+"steps/*/packagekit-observations.json")):
    for rec in json.load(open(f)):
        for k in ("before","after"):
            x=rec.get(k) or {}
            for c in x.get("context_processes",[]):
                key=(c["comm"],c["cmdline"])
                if key not in seen: seen.add(key); print(f.split("/steps/")[1][:12],rec["label"],x.get("at","")[11:19],c)
            for d in x.get("packagekitd",[]):
                key=("pk",d["pid"],tuple(d["backend_pathnames"] or []))
                if key not in seen: seen.add(key); print("pk",x.get("at","")[11:19],{k2:d[k2] for k2 in ("pid","etime_s","backend_pathnames","apt_backend","grep_c_aptcc","aptcc_backend","children","holds_or_waits","rule_idle")})
PY
