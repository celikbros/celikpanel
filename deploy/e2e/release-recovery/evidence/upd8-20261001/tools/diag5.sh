#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
cd $ev/steps
grep -h -i -E 'mail|postfix|dovecot|vmail|fail|error' */journal-product.txt */journal-setup-services.txt 2>/dev/null | grep -v 'TLS handshake' | cut -c1-400 | tail -40
python3 -c "
import json
d=json.load(open('06-setup/step.json'))
print(json.dumps(d['checks'].get('owner_attempts'),indent=0)[:3000])
print(json.dumps(d['checks'].get('owner_idle_waits'))[:1500])
"
