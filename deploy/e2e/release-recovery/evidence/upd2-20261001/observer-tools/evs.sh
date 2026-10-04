#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
c=$(ls -d $ev/steps/*-collect)
python3 - "$c" <<'PY'
import json,sys
c=sys.argv[1]
for e in json.load(open(c+'/observer-events.json')):
    print('OBS',e.get('at') or e.get('utc'),e.get('event'),json.dumps({k:v for k,v in e.items() if k not in ('schema','identity','operation_id','event','at','utc')})[:300])
PY
echo "--- recovery-fault-events"
[ -f $c/recovery-fault-events.jsonl ] && python3 -c "
import json,sys
for l in open('$c/recovery-fault-events.jsonl'):
    e=json.loads(l); print(e.get('at') or e.get('utc'), e.get('event'), json.dumps({k:v for k,v in e.items() if k in ('checkpoint','phase','snapshot','pid','boot_id','worker')})[:250])
"
echo "--- product journal key lines"
grep -nE "CELIKPANEL_UPDATE|phase|active|rollback|payload_restored|runtime_verified|attempt|retry|admission|recover" $c/journal-product.txt | grep -v "GET /api" | cut -c1-260 | head -${3:-70}
