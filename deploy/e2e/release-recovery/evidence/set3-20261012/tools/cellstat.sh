#!/bin/bash
# usage: cellstat.sh JOB LAB  -> job state, the step lines so far, and the newest evidence files
L=/var/tmp/cp-set3-run/logs; n=$1; lab=/var/tmp/cp-release-drill-$2
echo "now=$(date -u +%FT%TZ) start=$(cat $L/$n.start 2>/dev/null) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
grep -h '^{"step"' $L/$n.out 2>/dev/null | cut -c1-${3:-330}
echo "--- last out"; tail -n 2 $L/$n.out 2>/dev/null | cut -c1-300
echo "--- err"; tail -n 3 $L/$n.err 2>/dev/null | cut -c1-300
ls -t $lab/evidence/*/upd1/*/steps 2>/dev/null | head -n 3 | tr '\n' ' '; echo
free -m | sed -n 2p
