#!/bin/bash
# usage: cellstat.sh SHORT [LINES]
L=/var/tmp/cp-set1-run/logs; n=cell-$1; k=${2:-6}
echo "now=$(date -u +%FT%TZ) start=$(cat $L/$n.start 2>/dev/null) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
grep -E '^\{"step"' $L/$n.out 2>/dev/null | tail -n $k | cut -c1-700
tail -n 2 $L/$n.err 2>/dev/null | cut -c1-300
