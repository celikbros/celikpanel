#!/bin/bash
# set5: bounded status of one job. usage: cellstat.sh JOB
L=/var/tmp/cp-set5-run/logs; j=$1
grep -h '^{"step"' $L/$j.out 2>/dev/null | cut -c1-330 | tail -n ${2:-30}
echo "--- out (non-step, last 4)"; grep -v '^{"step"' $L/$j.out 2>/dev/null | tail -n 4 | cut -c1-300
echo "--- err tail"; tail -n 6 $L/$j.err 2>/dev/null | cut -c1-400
echo "rc=$(cat $L/$j.rc 2>/dev/null) start=$(cat $L/$j.start 2>/dev/null) end=$(cat $L/$j.end 2>/dev/null) now=$(date -u +%T)"
tail -n 3 /var/tmp/cp-set5-run/ram-nodes.txt 2>/dev/null | cut -c1-200
tail -n 2 /var/tmp/cp-set5-run/c-drive-cells.txt 2>/dev/null
free -m | sed -n 2,3p
