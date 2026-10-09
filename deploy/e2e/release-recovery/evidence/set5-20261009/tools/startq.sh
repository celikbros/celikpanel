#!/bin/bash
# set5. usage: startq.sh NAME ITEM...   -> starts one detached queue worker named NAME (one at a time)
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
name=$1; shift
L=/var/tmp/cp-set5-run/logs
pgrep -f "set5/queue.sh" > /dev/null && { echo "refusing: a queue worker is running"; exit 2; }
pgrep -f "run-set5.sh cell" > /dev/null && { echo "refusing: a cell is running"; exit 2; }
setsid nohup bash $J/queue.sh "$@" > $L/queue-$name.out 2>&1 < /dev/null &
disown
sleep 1
echo "queue $name started"
