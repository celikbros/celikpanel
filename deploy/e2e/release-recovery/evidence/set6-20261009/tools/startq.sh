#!/bin/bash
# set6. usage: startq.sh NAME ITEM...   -> starts one detached queue worker named NAME (one at a time)
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
name=$1; shift
L=/var/tmp/cp-set6-run/logs
pgrep -f "set6/queue.sh" > /dev/null && { echo "refusing: a queue worker is running"; exit 2; }
pgrep -f "run-set6.sh cell" > /dev/null && { echo "refusing: a cell is running"; exit 2; }
pgrep qemu > /dev/null && { echo "refusing: a QEMU process is running"; exit 2; }
setsid nohup bash $J/queue.sh "$@" > $L/queue-$name.out 2>&1 < /dev/null &
disown
sleep 1
echo "queue $name started"
