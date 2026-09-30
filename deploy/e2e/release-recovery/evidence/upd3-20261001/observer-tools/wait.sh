#!/bin/bash
# usage: wait.sh JOB [SECONDS]  -- block until the job ends or SECONDS pass; print step lines
L=/var/tmp/cp-upd3-run/logs
job=$1; lim=${2:-540}
t0=$(date +%s)
while [ ! -f $L/$job.end ] && [ $(( $(date +%s) - t0 )) -lt $lim ]; do sleep 5; done
date -u +%FT%TZ
grep '"step"' $L/$job.out | cut -c1-500
[ -f $L/$job.end ] && { echo "END $job rc=$(cat $L/$job.rc) at $(cat $L/$job.end)"; tail -n 1 $L/$job.out | cut -c1-300; tail -n 3 $L/$job.err | cut -c1-300; }
ls /var/tmp/cp-upd3-run/side/ 2>/dev/null | head -0
exit 0
