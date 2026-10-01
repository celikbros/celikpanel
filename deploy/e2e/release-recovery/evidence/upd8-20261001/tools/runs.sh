#!/bin/bash
cd /var/tmp/cp-upd8-run/logs
for n in ubgood ubgoodb ubgoodc ubgoodd uboc ubdef; do echo "$n $(cat cell-$n.start 2>/dev/null) $(cat cell-$n.end 2>/dev/null) rc=$(cat cell-$n.rc 2>/dev/null) $(cat /var/tmp/cp-upd8-run/jobs/job-cell-$n.harness)"; done
for l in "$@"; do python3 - "$l" <<'PY'
import json,glob,sys
ev=sorted(glob.glob('/var/tmp/cp-release-drill-%s/evidence/*/upd1/*/'%sys.argv[1]))[-1]
r=json.load(open(ev+'result.json')); o=r.get('outcome')
print(sys.argv[1], ev.split('/')[-2], r['overall'], o.get('classification') if isinstance(o,dict) else o)
for s in r['steps']:
    if s['verdict'] not in ('passed','skipped'): print('  ', s['name'], s['verdict'], (s.get('reason') or '')[:250])
PY
done
