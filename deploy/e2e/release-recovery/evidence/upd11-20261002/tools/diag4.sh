#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
echo $ev; ls $ev/steps/06-setup $ev/steps/*collect | head -40
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
d=json.load(open(glob.glob(ev+"steps/06-setup/step.json")[0]))
print(d.get("started_at"),d.get("finished_at"),d.get("verdict"),d.get("reason"))
c=d.get("checks",{})
print(sorted(c.keys()))
for k in ("owner_attempts","owner_idle_waits"):
    print(k, json.dumps(c.get(k))[:2500])
PY
