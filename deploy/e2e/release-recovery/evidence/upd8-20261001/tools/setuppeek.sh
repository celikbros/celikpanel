#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
date -u +%T
ls $ev/steps/06-setup | grep -v '^api$' | tr '\n' ' '; echo
for f in $ev/steps/06-setup/setup-execution-attempt-*.json; do [ -e "$f" ] && python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(sys.argv[1][-12:],d.get('status'),d.get('phase'),(d.get('error') or {}).get('code'),{s['id']:s['status'] for s in d.get('steps',[])})" $f; done
last=$(ls $ev/steps/06-setup/api | grep operation | tail -1); echo "api calls: $(ls $ev/steps/06-setup/api | wc -l) last=$last"
[ -n "$last" ] && python3 -c "
import json,sys
d=json.load(open(sys.argv[1]))
b=d.get('response',{}).get('body') or d.get('body') or {}
if isinstance(b,str):
    try: b=json.loads(b)
    except Exception: pass
print(json.dumps(b)[:600])" $ev/steps/06-setup/api/$last
