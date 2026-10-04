#!/bin/bash
S=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd8
python3 $S/worksum.py upd8-ub-def-a | cut -c1-400
python3 - <<'PY'
import json,glob
ev=sorted(glob.glob('/var/tmp/cp-release-drill-upd8-ub-def-a/evidence/*/upd1/*/'))[-1]
r=json.load(open(ev+'result.json')); o=r['outcome']
print(json.dumps({k:o.get(k) for k in ('reboot','classification','pin_changes')})[:1500])
c=json.load(open(sorted(glob.glob(ev+'steps/*-terminal/step.json'))[-1]))['checks']
print({k:c.get(k) for k in ('outcome','site_marker','smtp','firewall_equal','timers','login_ok','seeded_rows','mailbox','running_matches_installed','build_identity_ok')})
print(json.dumps(c.get('builds'))[:600]); print(json.dumps(c.get('database'))[:300]); print(json.dumps(c.get('floor'))[:300])
u=c.get('update_card') or {}
print('status', json.dumps(u.get('status'))[:700]); print('check', json.dumps(u.get('check'))[:400]); print('recovery', json.dumps(u.get('recovery'))[:300])
print('judged', json.dumps(u.get('update_card_judged'))[:400])
PY
cd /var/tmp/cp-release-drill-upd8-ub-def-a/evidence/ubuntu/upd1/*/steps/14-collect
grep -h -E '16:(4[89]|50):' journal-product.txt | grep -v 'TLS handshake' | grep -i -E 'reset|Started celikpanel-(agent|panel)|Panel ready|Starting CelikPanel Agent|Rollback complete|another server change|Stopped celikpanel|boot' | cut -c1-260 | head -30
