#!/bin/bash
# set7: print a lab's guest timeline (probe transitions, panel version) and the celikpanel unit start/stop journal lines
d=$(ls -d /var/tmp/cp-release-drill-$1/evidence/debian13/upd1/upd1-debian13-good-*/ | tail -1)
echo "$d"; ls $d/steps
t=$(ls -d $d/steps/*set7-guest-timeline)
python3 -I -c "
import json,sys
c=json.load(open(sys.argv[1]))['checks']
print(json.dumps({k:c.get(k) for k in ('probe_lines','probe_transitions','guest_minus_host_seconds','panel_version_after')}, indent=1))
" $t/step.json
grep -E "Stopp|Started|Starting|Deactivated|Main process exited|Succeeded|Failed" $t/journal-celikpanel-units.txt | grep -v "lab-set7-probe" | cut -c1-220 | tail -n ${2:-60}
