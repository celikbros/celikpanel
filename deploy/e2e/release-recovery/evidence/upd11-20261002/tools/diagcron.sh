#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
f=$ev/steps/16-management-off-measure/workload-management-off-after-reboot.json
python3 - $f <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
print(type(d), list(d.keys())[:30] if isinstance(d,dict) else len(d))
s=d.get("samples") if isinstance(d,dict) else d
if isinstance(s,list):
    print(len(s)); print(json.dumps(s[0])[:1500])
    for x in s:
        c=x.get("cron") or {}
        print(x.get("t"), x.get("boot_id","")[:8] if x.get("boot_id") else "", json.dumps(c)[:250])
else:
    print(json.dumps(d)[:3000])
PY
