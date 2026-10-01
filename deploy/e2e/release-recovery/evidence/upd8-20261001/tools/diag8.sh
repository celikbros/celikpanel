#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
d=json.load(open(ev+"steps/06-setup/step.json"))
a=d["checks"]["owner_attempts"][-1]
print(json.dumps(a.get("component"),indent=1)[:3000])
for f in sorted(glob.glob(ev+"steps/06-setup/api/*service-operation*")):
    print(f[-80:]); print(open(f).read()[:2500])
PY
