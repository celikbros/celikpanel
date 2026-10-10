#!/bin/bash
# usage: jobstat.sh NAME [LINES]
L=/var/tmp/cp-set1-run/logs; n=$1; k=${2:-8}
echo "start=$(cat $L/$n.start 2>/dev/null) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
echo "--- out"; tail -n $k $L/$n.out 2>/dev/null | cut -c1-400
echo "--- err"; tail -n 5 $L/$n.err 2>/dev/null | cut -c1-400
