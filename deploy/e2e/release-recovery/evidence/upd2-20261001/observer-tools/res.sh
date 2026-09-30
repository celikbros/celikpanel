#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
echo "EV=$ev"
python3 - "$ev" <<'PY'
import json,sys,glob
ev=sys.argv[1]
r=json.load(open(ev+'result.json'))
print('native_evidence',r['native_evidence'],'overall',r['overall'],'rid',r['request_id'])
for s in r['steps']: print(' %-32s %-12s %s %s %s'%(s['name'],s['verdict'],s['started_at'][11:],(s.get('finished_at') or '')[11:],(s.get('reason') or '')[:200]))
o=r['outcome']; print('outcome',o['classification'],'owner_cont',o['owner_continuation'])
print('attempts',json.dumps(o['attempts'])[:1500])
print('reboot',json.dumps(o['reboot']))
print('pin_changes',o['pin_changes'])
print('final',json.dumps(o['final_status']))
print('scope',json.dumps(r['scope'])[:1500])
print('findings'); [print('  -',f[:500]) for f in r['findings']]
for name in ('verdicts','terminal','track','owner-start','arm','pre-state'):
    d=glob.glob(ev+'steps/*-'+name)
    if not d: continue
    c=json.load(open(d[0]+'/step.json'))['checks']
    print('=== '+name)
    if name=='verdicts':
        for k,v in c['workloads'].items(): print('  ',k,json.dumps(v)[:700])
        print('  host_panel',json.dumps(c['host_panel_windows'])[:500]); print('  host_ssh',json.dumps(c['host_ssh_windows'])[:500])
        print('  agreement',json.dumps(c['agreement'])[:600]); print('  skew',c['clock_skew_seconds'])
    elif name=='terminal':
        for k in ('outcome','build_identity_ok','running_matches_installed','floor','foundation','transaction','database','timers','firewall_equal','site_marker','dns','mailbox','smtp','login_ok','seeded_rows','dns_scope'):
            print('  ',k,json.dumps(c.get(k))[:600])
        uc=c.get('update_card',{})
        for k in ('availability','version','check','status','recovery','license','cron'):
            print('  card',k,json.dumps(uc.get(k))[:500])
        print('  card recovery_guidance',json.dumps(uc.get('recovery_guidance'),ensure_ascii=False)[:1200])
        print('  card update_card',json.dumps(uc.get('update_card'),ensure_ascii=False)[:1200])
    elif name=='track':
        print('  ',json.dumps({k:c.get(k) for k in ('samples','agreement','reboot')})[:1500])
    elif name=='owner-start':
        print('  readiness',json.dumps(c.get('readiness'))[:400]); print('  waits',len(c.get('readiness_waits') or [])); print('  start',json.dumps(c.get('start'))[:300])
    elif name=='arm':
        print('  origin',json.dumps(c.get('origin_check'))[:400]); print('  check http',c['check']['http'],json.dumps(c['check']['body'])[:300])
    elif name=='pre-state':
        print('  ',json.dumps({k:v for k,v in c.items() if k!='sampler'})[:900])
PY
