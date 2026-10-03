#!/bin/bash
# usage: qwait.sh SECONDS -> waits until the queue log grows (or the limit), prints the new lines and the current step
L=/var/tmp/cp-upd13-run/logs/queue-main.out
M=/var/tmp/cp-upd13-run/qwait.mark
n0=$(cat $M 2>/dev/null || echo 0)
end=$(( $(date +%s) + ${1:-55} ))
while [ $(wc -l < $L) -le $n0 ] && [ $(date +%s) -lt $end ]; do sleep 5; done
n1=$(wc -l < $L); [ $n1 -gt $n0 ] && sed -n "$((n0+1)),${n1}p" $L | cut -c1-300
echo $n1 > $M
SN=1 bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13/qstat.sh 2>/dev/null | tail -n 3
