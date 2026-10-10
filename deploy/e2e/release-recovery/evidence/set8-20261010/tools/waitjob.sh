#!/bin/bash
# set8: wait until a background job has ended (or LIMIT seconds pass), then print its end line. usage: waitjob.sh NAME [LIMIT]
L=/var/tmp/cp-set8-run/logs; n=$1; limit=${2:-1700}; t=0
until [ -e $L/$n.end ] || [ $t -ge $limit ]; do sleep 10; t=$((t+10)); done
echo "job=$n end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) waited=${t}s"
grep -h '^{"step"' $L/$n.out 2>/dev/null | cut -c1-200 | tail -n 30
tail -n 3 $L/$n.out 2>/dev/null | cut -c1-300
