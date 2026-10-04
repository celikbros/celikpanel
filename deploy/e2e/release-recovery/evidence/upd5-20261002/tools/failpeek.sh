#!/bin/bash
# usage: failpeek.sh LAB -> read-only: the stopped step's reason, the baseline result and the installer log tail
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
echo "== $ev"; ls $ev $ev/steps
python3 - "$ev" <<'PY'
import glob, json, sys
ev = sys.argv[1]
r = json.load(open(ev + "result.json"))
print("overall", r.get("overall"), "native_evidence", r.get("native_evidence"))
for s in r["steps"]:
    print(s["name"], s["verdict"], s.get("started_at"), s.get("finished_at"))
    if s["verdict"] not in ("passed", "observed", "skipped"):
        print("REASON:\n" + (s.get("reason") or ""))
for f in r.get("findings", []):
    print("finding:", f[:800])
st = sorted(glob.glob(ev + "steps/*-baseline-install/step.json"))
if st:
    print(json.dumps(json.load(open(st[0])).get("checks"), indent=1)[:4000])
PY
ls -la $L/
for f in $L/current-worker-baseline-*; do echo "== $f"; tail -n 60 "$f" | cut -c1-400; done
