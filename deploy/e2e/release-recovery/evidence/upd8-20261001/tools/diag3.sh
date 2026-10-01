#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
cd $ev/steps
cut -c1-300 14-collect/journal-product.txt | sed -n '1,50p'
cat 14-collect/journal-setup-services.txt | cut -c1-300
python3 -c "
import json,sys
d=json.load(open('06-setup/inspect-after-setup-01.json'))
print(list(d.keys()))
for k,v in d.items():
    s=json.dumps(v)
    if any(x in s for x in ('apt','dpkg','unattended','lock','busy')): print(k, s[:1500])
"
