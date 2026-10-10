#!/bin/bash
# set7: the journal lines of the celikpanel units in a window, without the periodic recovery-unit lines and the probe.
# usage: showjr.sh LAB FROM TO [PATTERN]
d=$(ls -d /var/tmp/cp-release-drill-$1/evidence/debian13/upd1/upd1-debian13-good-*/ | tail -1)
t=$(ls -d $d/steps/*set7-guest-timeline)
awk -v a="$2" -v b="$3" '{ if (substr($1,12,12) >= a && substr($1,12,12) <= b) print }' $t/journal-celikpanel-units.txt | grep -v "release-recovery.service\|lab-set7-probe" | grep -E "${4:-.}" | cut -c12-23,24-260
python3 -I -c "
import datetime,sys
for e in sys.argv[1:]: print(e, datetime.datetime.fromtimestamp(float(e), datetime.timezone.utc).isoformat())
" ${@:5}
