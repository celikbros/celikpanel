#!/bin/bash
# usage: last.sh LABNAME NODE  -- full body of the last track sample (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
f=$(ls $ev/steps/*-track*/samples/*.json | tail -1)
python3 - "$f" <<'PY'
import json,sys
s=json.load(open(sys.argv[1]))
print(json.dumps((s.get('update_status') or {}).get('body'),indent=1,ensure_ascii=False)[:3000])
print(json.dumps((s.get('recovery_api') or {}).get('body'),indent=1,ensure_ascii=False)[:2000])
c=s.get('cli') or {}
for l in ('en','tr'): print('--- cli',l); print((c.get(l) or {}).get('stdout'))
print('--- cli json'); print((c.get('json') or {}).get('stdout'))
PY
