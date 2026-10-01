#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1); echo $ev
ls $ev $ev/steps/*
python3 - $ev <<'PY'
import json,sys,glob
ev=sys.argv[1]
d=json.load(open(ev+"/steps/06-setup/setup-execution.json"))
print(json.dumps(d,indent=1)[:5000])
PY
