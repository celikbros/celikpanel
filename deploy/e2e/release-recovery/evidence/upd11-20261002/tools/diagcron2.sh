#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
f=$(ls $ev/steps/*collect/guest-samples.jsonl | tail -1)
python3 - $f $2 <<'PY'
import json,sys,datetime as dt
boot=sys.argv[2]
rows=[json.loads(l) for l in open(sys.argv[1]) if l.strip()]
w=[r for r in rows if r.get("boot_id","").startswith(boot)]
print("samples in boot", len(w), "all", len(rows))
last=None
for r in w:
    c=r.get("cron") or {}
    m=c.get("mtime")
    mt=dt.datetime.fromtimestamp(m, dt.timezone.utc).strftime("%H:%M:%S.%f")[:12] if m else None
    t=dt.datetime.fromtimestamp(r["t"], dt.timezone.utc).strftime("%H:%M:%S")
    if mt!=last or r is w[-1] or r is w[0]:
        print(t, "mono", r.get("monotonic"), "cron mtime", mt, "content", c.get("content"), "ok", c.get("ok"))
    last=mt
PY
