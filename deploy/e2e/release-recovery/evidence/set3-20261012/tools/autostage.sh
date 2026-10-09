#!/bin/bash
# set3: stage one cell that was started outside the queue as soon as its job has ended (detached).
# usage: autostage.sh JOB LAB NODE CELL RUN [PREFIX]
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
L=/var/tmp/cp-set3-run/logs
tr -d '\r' < $J/stage.sh > /var/tmp/cp-set3-run/stage.sh
setsid nohup bash -c "until [ -e $L/$1.end ]; do sleep 5; done; sleep 3; bash /var/tmp/cp-set3-run/stage.sh $* > $L/stage-$1.txt 2>&1; echo \"\$(date -u +%FT%TZ) manual staged $1 rc=\$? \$(grep -m1 '^overall' $L/stage-$1.txt)\" >> $L/queue.log" > /dev/null 2>&1 < /dev/null &
disown
sleep 1; echo "autostage armed for $1"
