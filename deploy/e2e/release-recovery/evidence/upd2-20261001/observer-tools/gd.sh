#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 - "$ev" <<'PY'
import json,sys,glob
ev=sys.argv[1]
seen={}
def add(kind,text,utc):
    k=(kind,text)
    if k not in seen: seen[k]=utc
for f in sorted(glob.glob(ev+'steps/*-track*/samples/*.json')):
    s=json.load(open(f)); u=s['utc'][11:19]
    for src in ('guidance_api','guidance_cli','update_card'):
        g=s.get(src)
        if not g: continue
        for lang in ('en','tr'):
            add(f'{src}.{lang}',' || '.join(t for t in g['texts'][lang] if t),u)
        if g.get('no_actor_or_action'): add(src+'.NO_ACTOR','missing '+str(g.get('missing_keys')),u)
    c=s.get('cli') or {}
    for lang in ('en','tr'):
        t=(c.get(lang) or {}).get('stdout','')
        t='\n'.join(l for l in t.splitlines() if not l.startswith(('Request:','İşlem:','Recorded at:','Kayıt zamanı:')))
        add('cli_raw.'+lang,t,u)
for k,u in sorted(seen.items(), key=lambda x:(x[0][0],x[1])):
    print(f'[{u}] {k[0]}: {k[1]}')
sh=glob.glob(ev+'steps/*-pre-state/offline-shell-pre-update.json')
if sh:
    o=json.load(open(sh[0])); print('OFFLINE page', json.dumps(o.get('page')), 'scripts', [ (x['path'],x['status'],x['carries_status_command']) for x in o.get('scripts',[])])
    print('OFFLINE texts', json.dumps(o.get('texts'),ensure_ascii=False)[:2500])
PY
