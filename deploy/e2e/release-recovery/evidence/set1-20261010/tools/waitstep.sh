#!/bin/bash
# usage: waitstep.sh SHORT STEP-NAME [SECONDS]  -> returns when the step line appears, the cell ended, or the limit
L=/var/tmp/cp-set1-run/logs; n=cell-$1; limit=${3:-3000}; t=0
until grep -q "\"step\": \"$2\"" $L/$n.out 2>/dev/null || [ -e $L/$n.end ] || [ $t -ge $limit ]; do sleep 10; t=$((t+10)); done
echo "now=$(date -u +%FT%TZ) waited=${t}s end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"
grep -E '^\{"step"' $L/$n.out | tail -n 12 | cut -c1-900
