#!/bin/bash
# usage: peek.sh SHORT LAB -> read-only: job state and the latest step records of the running cell.
short=$1; L=/var/tmp/cp-release-drill-$2
date -u +%FT%TZ
echo "start=$(cat /var/tmp/cp-upd7-run/logs/cell-$short.start) end=$(cat /var/tmp/cp-upd7-run/logs/cell-$short.end 2>/dev/null) rc=$(cat /var/tmp/cp-upd7-run/logs/cell-$short.rc 2>/dev/null)"
tail -n ${TAILN:-4} /var/tmp/cp-upd7-run/logs/cell-$short.err | cut -c1-300
tail -n ${TAILN:-4} /var/tmp/cp-upd7-run/logs/cell-$short.out | cut -c1-400
ev=$(ls -d $L/evidence/*/upd1/*/ 2>/dev/null | tail -1)
[ -n "$ev" ] && ls $ev/steps 2>/dev/null | tr '\n' ' '; echo
for f in $(ls $ev/steps/*/step.json 2>/dev/null); do python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(d.get('name'),d.get('verdict'),(d.get('reason') or '')[:300])" $f; done
s=$(ls -d $ev/steps/*track*/samples 2>/dev/null | tail -1)
if [ -n "$s" ]; then n=$(ls $s | wc -l); last=$(ls $s | tail -1); echo "track samples=$n last=$last"; python3 - "$s/$last" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
u=(d.get("update_status") or {}).get("body"); r=(d.get("recovery_api") or {})
cli=((d.get("cli") or {}).get("json") or {}).get("stdout")
print("utc",d.get("utc"),"update",json.dumps(u)[:300]); print("recovery_api http",r.get("http"),json.dumps(r.get("body"))[:300]); print("cli",(cli or "")[:500])
PY
fi
pgrep -c qemu-system >/dev/null && echo "qemu running: $(pgrep -c qemu-system)" || echo "no qemu"
