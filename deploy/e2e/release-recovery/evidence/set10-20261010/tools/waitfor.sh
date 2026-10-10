#!/bin/bash
# set10: wait until job NAME has printed a step line matching PATTERN or has ended (or LIMIT s). usage: waitfor.sh NAME PATTERN [LIMIT]
L=/var/tmp/cp-set10-run/logs; n=$1; pat=$2; limit=${3:-560}; t=0
until [ -e $L/$n.end ] || grep -q "$pat" $L/$n.out 2>/dev/null || [ $t -ge $limit ]; do sleep 10; t=$((t+10)); done
echo "job=$n end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null) waited=${t}s"
grep -h '^{"step"' $L/$n.out 2>/dev/null | cut -c1-260 | tail -n 8
tail -n 2 $L/$n.err 2>/dev/null | cut -c1-300
