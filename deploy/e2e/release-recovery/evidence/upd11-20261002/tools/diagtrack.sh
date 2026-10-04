#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
f=glob.glob(ev+"steps/*-track/step.json")[0]
d=json.load(open(f))
print(d.get("verdict"), d.get("reason"), d.get("started_at"), d.get("finished_at"))
c=d.get("checks") or {}
print(sorted(c.keys()))
for k in sorted(c.keys()):
    if k in ("samples",): continue
    print(k, json.dumps(c[k], ensure_ascii=False)[:700])
PY
