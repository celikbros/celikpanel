#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
for f in sorted(glob.glob(ev+"steps/*-track/samples/*.json")):
    d=json.load(open(f))
    a=d.get("agreement") or {}
    u=((d.get("update_status") or {}).get("body") or {})
    r=d.get("recovery_api") or {}
    rb=r.get("body") if isinstance(r.get("body"),dict) else {}
    try: cj=json.loads(((d.get("cli") or {}).get("json") or {}).get("stdout") or "null") or {}
    except ValueError: cj={}
    print(f.split("/")[-1], d.get("utc"), "agree=",a.get("verdict"), "panel=",u.get("status"), "api=",r.get("http"),rb.get("phase"),rb.get("reason"),rb.get("observation"), "cli=",cj.get("observation"),cj.get("phase"),cj.get("reason"), (json.dumps({k:v for k,v in a.items() if k!="verdict"})[:300]))
PY
