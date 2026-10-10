#!/bin/bash
# set6 read-only: wait until a step named PATTERN has been printed by job JOB, or the job ended, or LIMIT seconds
# passed; then print the job's step lines. usage: waitfor.sh JOB PATTERN [LIMIT_SECONDS]
L=/var/tmp/cp-set6-run/logs; job=$1; pattern=$2; limit=${3:-540}; t=0
until grep -q "\"step\": \"$pattern" $L/$job.out 2>/dev/null || [ -e $L/$job.end ] || [ $t -ge $limit ]; do sleep 5; t=$((t + 5)); done
grep -h '^{"step"' $L/$job.out 2>/dev/null | cut -c1-600 | tail -n ${4:-8}
echo "rc=$(cat $L/$job.rc 2>/dev/null) end=$(cat $L/$job.end 2>/dev/null) now=$(date -u +%T) waited=${t}s"
