#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
ls $ev/steps/*management-off* $ev/steps/*owner-reboot*
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
for n in ("management-off","owner-reboot","management-off-measure","management-return","kind-expectation"):
    for f in glob.glob(ev+f"steps/*-{n}/step.json"):
        d=json.load(open(f))
        print("=====",n,d.get("verdict"),d.get("reason"))
        print(json.dumps(d.get("checks"),sort_keys=True)[:3500])
PY
