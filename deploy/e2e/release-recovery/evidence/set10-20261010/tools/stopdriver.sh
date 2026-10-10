#!/bin/bash
# set10: stop this run's driver of one lab (the wrapper then stops the lab's guests). usage: stopdriver.sh LAB
pids=$(pgrep -f "set10_trial.py run .*cp-release-drill-$1 ")
echo "driver pids of $1: $pids"
[ -n "$pids" ] && kill -TERM $pids
sleep 5; pgrep -fa "set10_trial.py run .*cp-release-drill-$1 " || echo "driver of $1 ended"
