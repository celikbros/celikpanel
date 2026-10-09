#!/bin/bash
# read-only: wait up to 50 s for a new step line or a job end, then print one line per cell job
L=/var/tmp/cp-set2-run/logs
count() { cat $L/cell-*.out 2>/dev/null | grep -c '^{"step"'; ls $L/cell-*.end 2>/dev/null | wc -l; }
a=$(count | tr '\n' ' ')
for i in $(seq 1 25); do b=$(count | tr '\n' ' '); [ "$a" != "$b" ] && break; sleep 2; done
for f in $L/cell-*.start; do n=$(basename $f .start); echo "$n rc=$(cat $L/$n.rc 2>/dev/null) steps=$(grep -c '^{"step"' $L/$n.out) last=$(grep '^{"step"' $L/$n.out | tail -n 1 | cut -c1-${1:-260})"; done
date -u +%T; free -m | sed -n 2p | cut -c1-60
