#!/bin/bash
# usage: startq.sh WORKER GATE ITEM...   -> one queue worker, detached
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
L=/var/tmp/cp-set3-run/logs
tr -d '\r' < $J/queue.sh > /var/tmp/cp-set3-run/queue.sh
tr -d '\r' < $J/stage.sh > /var/tmp/cp-set3-run/stage.sh
setsid nohup bash /var/tmp/cp-set3-run/queue.sh "$@" > $L/queue-$1.out 2>&1 < /dev/null &
disown
sleep 1; echo "worker $1 started"
